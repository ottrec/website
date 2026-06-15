package ottrectm

// This file implements structural diffing of two indexed datasets, for the time
// machine's diff and per-facility timeline pages. The goal is a compact,
// human-readable summary of what changed between two snapshots, given that the
// raw volume of changes is large and dominated by a few recurring patterns:
//
//   - a schedule duplicated forward with a new (future) date range, sometimes
//     with a handful of times tweaked;
//   - a schedule replaced wholesale by a mostly-different one;
//   - a single time moved or added/removed;
//   - special-hours / notification / schedule-change text edited.
//
// Identity is keyed off stable handles where possible: facilities by source URL,
// schedule groups by title, activities by name, and time slots by weekday + clock
// range. Schedules have no stable key (a group routinely holds several with the
// same name and different date ranges), so they are matched by grid similarity
// with copy detection, and only ever shown as a delta against the schedule they
// most resemble.

import (
	"cmp"
	"hash/fnv"
	"io"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/pgaskin/ottrec-website/pkg/ottrecidx"
	"github.com/pgaskin/ottrec/schema"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Change is the kind of change applied to an object between two snapshots.
type Change int

const (
	Unchanged Change = iota
	Added
	Removed
	Modified
)

func (c Change) String() string {
	switch c {
	case Added:
		return "added"
	case Removed:
		return "removed"
	case Modified:
		return "modified"
	default:
		return "unchanged"
	}
}

// thresholds for schedule matching (Jaccard similarity over time-slot signatures)
const (
	// schedules at or above this similarity are treated as the same schedule
	// continuing to exist (possibly modified) rather than a remove + add.
	pairThreshold = 0.25
	// an added/removed schedule at or above this similarity to another schedule
	// is shown as a copy/duplicate delta instead of dumping the whole grid.
	copyThreshold = 0.5
)

// DataDiff is the diff between two datasets (old → new).
type DataDiff struct {
	Facilities []FacilityDiff // every facility (matched by source URL); use Changed for those that differ
}

// Changed returns only the facilities that changed.
func (d *DataDiff) Changed() []FacilityDiff {
	var out []FacilityDiff
	for _, f := range d.Facilities {
		if f.Change != Unchanged {
			out = append(out, f)
		}
	}
	return out
}

// Counts returns the number of added, removed, and modified facilities.
func (d *DataDiff) Counts() (added, removed, modified int) {
	for _, f := range d.Facilities {
		switch f.Change {
		case Added:
			added++
		case Removed:
			removed++
		case Modified:
			modified++
		}
	}
	return
}

// FacilityDiff is the diff of a single facility. Old or New may be invalid for
// added/removed facilities.
type FacilityDiff struct {
	Change        Change
	URL           string
	Name          string
	Old, New      ottrecidx.FacilityRef
	SpecialHours  TextDiff
	Notifications TextDiff
	Errors        ErrorsDiff  // scrape errors added/removed
	Groups        []GroupDiff // only groups that changed
}

// ErrorsDiff is the change to a facility's scrape errors (e.g. failed time
// parsing), as a set diff.
type ErrorsDiff struct {
	Change  Change
	Added   []string
	Removed []string
}

// GroupDiff is the diff of a schedule group (matched by title). Old or New may
// be invalid for added/removed groups.
type GroupDiff struct {
	Change          Change
	Title           string
	Old, New        ottrecidx.ScheduleGroupRef
	ScheduleChanges TextDiff
	Reservation     ResvDiff
	Schedules       []ScheduleDiff // only schedules that changed
}

// ResvDiff is the change to a group's reservation links.
type ResvDiff struct {
	Change  Change
	Added   []ottrecidx.ReservationLink
	Removed []ottrecidx.ReservationLink
}

// ScheduleDiff is the diff of a single schedule. Because schedules have no
// stable identity, this describes the change as a delta from a baseline (the
// matched old schedule, or — for copies — the schedule it most resembles).
type ScheduleDiff struct {
	Change      Change
	Name        string
	Old, New    ottrecidx.ScheduleRef
	OldDate     string // effective date-range label of the old side
	NewDate     string // effective date-range label of the new side
	DateChanged bool
	Fixed       bool // a fixed-date (e.g. holiday-week) schedule

	// RefDate, if set, is the date-range label of the schedule this one is a
	// copy/duplicate of (for added or removed schedules shown as a delta).
	RefDate string
	// Identical is true when the delta against the baseline is empty (a pure
	// date-range change, or an exact copy).
	Identical bool

	// Activities holds the per-activity grid delta, for modified schedules and
	// copies. Only changed activities/days are included.
	Activities []ActivityDiff
	// ShowFull is set for genuinely new/removed schedules with no useful
	// baseline; the renderer should show the whole grid (from New or Old).
	ShowFull bool
}

// ActivityDiff is the diff of a single activity row within a schedule.
type ActivityDiff struct {
	Change   Change
	Name     string
	Old, New ottrecidx.ActivityRef
	Days     []DayDiff // only days that changed
}

// DayDiff is the set of time slots added and/or removed for one day of an
// activity. A day with exactly one removed and one added slot is a moved/changed
// time.
type DayDiff struct {
	Label   string
	Removed []string
	Added   []string
}

// TextDiff is a line-level diff of a scraped HTML fragment (special hours,
// notifications, schedule changes).
type TextDiff struct {
	Change   Change
	Old, New string // raw HTML, trimmed
	Lines    []TextLine
}

// TextLine is one line of a [TextDiff], with Change one of Unchanged (context),
// Added, or Removed.
type TextLine struct {
	Change Change
	Text   string
}

// Compare computes the diff between two datasets (old → new), matching
// facilities by source URL.
func Compare(old, new ottrecidx.DataRef) *DataDiff {
	oldByKey := facilitiesByKey(old)
	newByKey, order := facilitiesByKeyOrdered(new)
	for k := range oldByKey {
		if _, ok := newByKey[k]; !ok {
			order = append(order, k)
		}
	}
	dd := &DataDiff{}
	for _, k := range order {
		dd.Facilities = append(dd.Facilities, CompareFacility(oldByKey[k], newByKey[k]))
	}
	return dd
}

// CompareFacility diffs a single facility. Either ref may be the zero value to
// signify the facility is absent on that side.
func CompareFacility(old, new ottrecidx.FacilityRef) FacilityDiff {
	fd := FacilityDiff{Old: old, New: new}
	if new.Valid() {
		fd.Name, fd.URL = new.GetName(), new.GetSourceURL()
	}
	if old.Valid() {
		if fd.Name == "" {
			fd.Name = old.GetName()
		}
		if fd.URL == "" {
			fd.URL = old.GetSourceURL()
		}
	}

	var oldSH, oldNT, newSH, newNT string
	if old.Valid() {
		oldSH, oldNT = old.GetSpecialHoursHTML(), old.GetNotificationsHTML()
	}
	if new.Valid() {
		newSH, newNT = new.GetSpecialHoursHTML(), new.GetNotificationsHTML()
	}
	fd.SpecialHours = textDiff(oldSH, newSH)
	fd.Notifications = textDiff(oldNT, newNT)
	fd.Errors = errorsDiff(old, new)
	fd.Groups = diffGroups(old, new)

	switch {
	case !old.Valid():
		fd.Change = Added
	case !new.Valid():
		fd.Change = Removed
	case fd.SpecialHours.Change != Unchanged, fd.Notifications.Change != Unchanged, fd.Errors.Change != Unchanged, len(fd.Groups) > 0:
		fd.Change = Modified
	default:
		fd.Change = Unchanged
	}
	return fd
}

// errorsDiff computes the set difference of two facilities' scrape errors.
func errorsDiff(old, new ottrecidx.FacilityRef) ErrorsDiff {
	oldSet := map[string]bool{}
	newSet := map[string]bool{}
	if old.Valid() {
		for e := range old.GetErrors() {
			oldSet[e] = true
		}
	}
	if new.Valid() {
		for e := range new.GetErrors() {
			newSet[e] = true
		}
	}
	var ed ErrorsDiff
	for e := range newSet {
		if !oldSet[e] {
			ed.Added = append(ed.Added, e)
		}
	}
	for e := range oldSet {
		if !newSet[e] {
			ed.Removed = append(ed.Removed, e)
		}
	}
	sort.Strings(ed.Added)
	sort.Strings(ed.Removed)
	if len(ed.Added) > 0 || len(ed.Removed) > 0 {
		ed.Change = Modified
	}
	return ed
}

func facilitiesByKey(d ottrecidx.DataRef) map[string]ottrecidx.FacilityRef {
	m, _ := facilitiesByKeyOrdered(d)
	return m
}

func facilitiesByKeyOrdered(d ottrecidx.DataRef) (map[string]ottrecidx.FacilityRef, []string) {
	m := map[string]ottrecidx.FacilityRef{}
	var order []string
	if !d.Valid() {
		return m, order
	}
	for f := range d.Facilities() {
		k := FacilityKey(f)
		if _, ok := m[k]; !ok {
			order = append(order, k)
		}
		m[k] = f
	}
	return m, order
}

// FacilityKey identifies a facility across datasets by its source URL, falling
// back to its name when no URL is available.
func FacilityKey(f ottrecidx.FacilityRef) string {
	if u := strings.TrimSpace(f.GetSourceURL()); u != "" {
		return u
	}
	return "name:" + f.GetName()
}

// diffGroups matches schedule groups by title and returns those that changed.
func diffGroups(old, new ottrecidx.FacilityRef) []GroupDiff {
	type grp = ottrecidx.ScheduleGroupRef
	oldByKey := map[string]grp{}
	newByKey := map[string]grp{}
	var order []string
	seen := map[string]bool{}
	if new.Valid() {
		for g := range new.ScheduleGroups() {
			k := groupTitle(g)
			if !seen[k] {
				seen[k] = true
				order = append(order, k)
			}
			newByKey[k] = g
		}
	}
	if old.Valid() {
		for g := range old.ScheduleGroups() {
			k := groupTitle(g)
			oldByKey[k] = g
			if !seen[k] {
				seen[k] = true
				order = append(order, k)
			}
		}
	}

	var out []GroupDiff
	for _, k := range order {
		o, ook := oldByKey[k]
		n, nok := newByKey[k]
		gd := GroupDiff{Title: k, Old: o, New: n}

		var oldSC, newSC string
		if ook {
			oldSC = o.GetScheduleChangesHTML()
		}
		if nok {
			newSC = n.GetScheduleChangesHTML()
		}
		gd.ScheduleChanges = textDiff(oldSC, newSC)
		gd.Reservation = resvDiff(o, n)
		gd.Schedules = diffSchedules(o, n)

		switch {
		case !ook:
			gd.Change = Added
		case !nok:
			gd.Change = Removed
		case gd.ScheduleChanges.Change != Unchanged, gd.Reservation.Change != Unchanged, len(gd.Schedules) > 0:
			gd.Change = Modified
		default:
			gd.Change = Unchanged
		}
		if gd.Change != Unchanged {
			out = append(out, gd)
		}
	}
	return out
}

func groupTitle(g ottrecidx.ScheduleGroupRef) string {
	if s := strings.TrimSpace(g.GetTitle()); s != "" {
		return s
	}
	return strings.TrimSpace(g.GetLabel())
}

func resvDiff(old, new ottrecidx.ScheduleGroupRef) ResvDiff {
	key := func(l ottrecidx.ReservationLink) string { return l.URL + "\x00" + l.Label }
	oldSet := map[string]ottrecidx.ReservationLink{}
	newSet := map[string]ottrecidx.ReservationLink{}
	if old.Valid() {
		for l := range old.GetReservationLinks() {
			oldSet[key(l)] = l
		}
	}
	if new.Valid() {
		for l := range new.GetReservationLinks() {
			newSet[key(l)] = l
		}
	}
	var rd ResvDiff
	for k, l := range newSet {
		if _, ok := oldSet[k]; !ok {
			rd.Added = append(rd.Added, l)
		}
	}
	for k, l := range oldSet {
		if _, ok := newSet[k]; !ok {
			rd.Removed = append(rd.Removed, l)
		}
	}
	sortLinks(rd.Added)
	sortLinks(rd.Removed)
	if len(rd.Added) > 0 || len(rd.Removed) > 0 {
		rd.Change = Modified
	}
	return rd
}

func sortLinks(ls []ottrecidx.ReservationLink) {
	sort.Slice(ls, func(i, j int) bool {
		return cmp.Or(cmp.Compare(ls[i].Label, ls[j].Label), cmp.Compare(ls[i].URL, ls[j].URL)) < 0
	})
}

// diffSchedules matches the schedules of two groups and returns the changed
// ones. Regular (recurring weekday) and fixed-date (e.g. holiday-week) schedules
// are matched within separate pools so a holiday schedule is never paired with a
// regular one.
func diffSchedules(old, new ottrecidx.ScheduleGroupRef) []ScheduleDiff {
	var olds, news []ottrecidx.ScheduleRef
	if old.Valid() {
		for s := range old.Schedules() {
			olds = append(olds, s)
		}
	}
	if new.Valid() {
		for s := range new.Schedules() {
			news = append(news, s)
		}
	}

	oldReg, oldFix := partitionFixed(olds)
	newReg, newFix := partitionFixed(news)

	var out []ScheduleDiff
	out = append(out, matchRegular(oldReg, newReg)...)
	out = append(out, matchFixed(oldFix, newFix)...)
	return out
}

func partitionFixed(ss []ottrecidx.ScheduleRef) (regular, fixed []ottrecidx.ScheduleRef) {
	for _, s := range ss {
		if isFixedDate(s) {
			fixed = append(fixed, s)
		} else {
			regular = append(regular, s)
		}
	}
	return
}

// isFixedDate reports whether a schedule's columns are concrete calendar dates
// (a one-off / holiday-week schedule) rather than recurring weekdays.
func isFixedDate(s ottrecidx.ScheduleRef) bool {
	n := s.NumDays()
	if n == 0 {
		return false
	}
	var dated int
	for i := range n {
		if d, ok := s.GetDayDate(i); ok {
			_, hasMonth := d.Month()
			_, hasDay := d.Day()
			if hasMonth && hasDay {
				dated++
			}
		}
	}
	return dated*2 >= n
}

// matchRegular matches regular schedules by grid similarity, with copy detection
// for the leftovers.
func matchRegular(olds, news []ottrecidx.ScheduleRef) []ScheduleDiff {
	oldSig := signatures(olds)
	newSig := signatures(news)

	oldUsed := make([]bool, len(olds))
	newUsed := make([]bool, len(news))
	pairs := map[int]int{} // new index → old index

	// candidate pairs, best similarity first; ties broken by closer date ranges.
	type cand struct {
		oi, ni int
		sim    float64
		dist   int
	}
	var cands []cand
	for ni := range news {
		for oi := range olds {
			cands = append(cands, cand{oi, ni, jaccard(oldSig[oi], newSig[ni]), dateDistance(olds[oi], news[ni])})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].sim != cands[j].sim {
			return cands[i].sim > cands[j].sim
		}
		return cands[i].dist < cands[j].dist
	})
	for _, c := range cands {
		if c.sim < pairThreshold || oldUsed[c.oi] || newUsed[c.ni] {
			continue
		}
		oldUsed[c.oi] = true
		newUsed[c.ni] = true
		pairs[c.ni] = c.oi
	}
	// force-pair leftovers sharing an identical date range (same slot, grid
	// rewritten) so they show as a modification rather than remove + add.
	for ni := range news {
		if newUsed[ni] {
			continue
		}
		for oi := range olds {
			if oldUsed[oi] {
				continue
			}
			if schedDateLabel(olds[oi]) == schedDateLabel(news[ni]) && schedDateLabel(news[ni]) != "" {
				oldUsed[oi], newUsed[ni] = true, true
				pairs[ni] = oi
				break
			}
		}
	}

	var out []ScheduleDiff
	// emit in new order, then trailing removed in old order.
	for ni := range news {
		n := news[ni]
		if oi, ok := pairs[ni]; ok {
			if sd, changed := modifiedSchedule(olds[oi], n); changed {
				out = append(out, sd)
			}
			continue
		}
		out = append(out, addedSchedule(n, newSig[ni], olds, oldSig, news, newSig, ni))
	}
	for oi := range olds {
		if oldUsed[oi] {
			continue
		}
		out = append(out, removedSchedule(olds[oi], oldSig[oi], news, newSig))
	}
	return out
}

// matchFixed matches fixed-date schedules by identical date range only; the rest
// are treated as standalone additions/removals.
func matchFixed(olds, news []ottrecidx.ScheduleRef) []ScheduleDiff {
	oldUsed := make([]bool, len(olds))
	var out []ScheduleDiff
	for _, n := range news {
		matched := -1
		for oi, o := range olds {
			if !oldUsed[oi] && schedDateLabel(o) == schedDateLabel(n) {
				matched = oi
				break
			}
		}
		if matched >= 0 {
			oldUsed[matched] = true
			if sd, changed := modifiedSchedule(olds[matched], n); changed {
				out = append(out, sd)
			}
			continue
		}
		out = append(out, ScheduleDiff{Change: Added, Name: schedName(n), New: n, NewDate: schedDateLabel(n), Fixed: true, ShowFull: true})
	}
	for oi, o := range olds {
		if oldUsed[oi] {
			continue
		}
		out = append(out, ScheduleDiff{Change: Removed, Name: schedName(o), Old: o, OldDate: schedDateLabel(o), Fixed: true, ShowFull: true})
	}
	return out
}

// modifiedSchedule builds the diff for a matched pair, returning changed=false if
// nothing changed.
func modifiedSchedule(old, new ottrecidx.ScheduleRef) (ScheduleDiff, bool) {
	sd := ScheduleDiff{
		Change:  Modified,
		Name:    schedName(new),
		Old:     old,
		New:     new,
		OldDate: schedDateLabel(old),
		NewDate: schedDateLabel(new),
		Fixed:   isFixedDate(new),
	}
	sd.DateChanged = sd.OldDate != sd.NewDate
	sd.Activities = diffGrid(old, new)
	sd.Identical = len(sd.Activities) == 0
	if !sd.DateChanged && sd.Identical {
		return ScheduleDiff{}, false
	}
	return sd, true
}

// addedSchedule builds the diff for a new schedule with no match, expressing it
// as a copy delta when it closely resembles another schedule.
func addedSchedule(n ottrecidx.ScheduleRef, nsig map[string]int, olds []ottrecidx.ScheduleRef, oldSig []map[string]int, news []ottrecidx.ScheduleRef, newSig []map[string]int, ni int) ScheduleDiff {
	sd := ScheduleDiff{Change: Added, Name: schedName(n), New: n, NewDate: schedDateLabel(n)}

	// best reference among old schedules and other (earlier) new schedules.
	ref, refSig, refDate, best := ottrecidx.ScheduleRef{}, map[string]int(nil), "", 0.0
	for oi, o := range olds {
		if s := jaccard(oldSig[oi], nsig); s > best {
			best, ref, refSig, refDate = s, o, oldSig[oi], schedDateLabel(o)
		}
	}
	for j, o := range news {
		if j == ni {
			continue
		}
		if s := jaccard(newSig[j], nsig); s > best {
			best, ref, refSig, refDate = s, o, newSig[j], schedDateLabel(o)
		}
	}
	if best >= copyThreshold && ref.Valid() {
		_ = refSig
		sd.RefDate = refDate
		sd.Activities = diffGrid(ref, n)
		sd.Identical = len(sd.Activities) == 0
		return sd
	}
	sd.ShowFull = true
	return sd
}

// removedSchedule builds the diff for a removed schedule, noting when it merely
// duplicated a surviving one. A removed schedule is never shown as a +/- delta
// (that would read as edits to something that no longer exists, and the delta
// would be relative to the survivor rather than to a prior version): an exact
// duplicate shows just the note, and a near-duplicate shows its full grid.
func removedSchedule(o ottrecidx.ScheduleRef, osig map[string]int, news []ottrecidx.ScheduleRef, newSig []map[string]int) ScheduleDiff {
	sd := ScheduleDiff{Change: Removed, Name: schedName(o), Old: o, OldDate: schedDateLabel(o)}
	ref, refDate, best := ottrecidx.ScheduleRef{}, "", 0.0
	for j, n := range news {
		if s := jaccard(newSig[j], osig); s > best {
			best, ref, refDate = s, n, schedDateLabel(n)
		}
	}
	if best >= copyThreshold && ref.Valid() {
		sd.RefDate = refDate
		sd.Identical = len(diffGrid(ref, o)) == 0 // exactly duplicated the survivor
		sd.ShowFull = !sd.Identical               // otherwise show what was removed, in full
		return sd
	}
	sd.ShowFull = true
	return sd
}

// diffGrid computes the per-activity, per-day delta from base to target. Only
// changed activities (and within them, changed days) are returned.
func diffGrid(base, target ottrecidx.ScheduleRef) []ActivityDiff {
	baseByName, baseOrder := activitiesByName(base)
	targetByName, targetOrder := activitiesByName(target)

	order := slices.Clone(targetOrder)
	for _, n := range baseOrder {
		if _, ok := targetByName[n]; !ok {
			order = append(order, n)
		}
	}

	var out []ActivityDiff
	for _, n := range order {
		b, bok := baseByName[n]
		t, tok := targetByName[n]
		switch {
		case !bok && tok:
			out = append(out, ActivityDiff{Change: Added, Name: n, New: t, Days: activityDaysOneSided(t, Added)})
		case bok && !tok:
			out = append(out, ActivityDiff{Change: Removed, Name: n, Old: b, Days: activityDaysOneSided(b, Removed)})
		default:
			if days := diffActivityDays(b, t); len(days) > 0 {
				out = append(out, ActivityDiff{Change: Modified, Name: n, Old: b, New: t, Days: days})
			}
		}
	}
	return out
}

func activitiesByName(s ottrecidx.ScheduleRef) (map[string]ottrecidx.ActivityRef, []string) {
	m := map[string]ottrecidx.ActivityRef{}
	var order []string
	for a := range s.Activities() {
		n := actName(a)
		if _, ok := m[n]; !ok {
			order = append(order, n)
			m[n] = a
		}
	}
	return m, order
}

// diffActivityDays diffs the day → time-slots maps of two activities.
func diffActivityDays(b, t ottrecidx.ActivityRef) []DayDiff {
	bOrder, bLabel, bTimes := activityDayTimes(b)
	tOrder, tLabel, tTimes := activityDayTimes(t)

	order := slices.Clone(tOrder)
	for _, k := range bOrder {
		if _, ok := tTimes[k]; !ok {
			order = append(order, k)
		}
	}

	var out []DayDiff
	for _, k := range order {
		rem := multisetDiff(bTimes[k], tTimes[k])
		add := multisetDiff(tTimes[k], bTimes[k])
		if len(rem) == 0 && len(add) == 0 {
			continue
		}
		lbl := tLabel[k]
		if lbl == "" {
			lbl = bLabel[k]
		}
		sort.Strings(rem)
		sort.Strings(add)
		out = append(out, DayDiff{Label: lbl, Removed: rem, Added: add})
	}
	return out
}

// activityDaysOneSided lists every day of an activity with all its times on one
// side, for a wholly added or removed activity row.
func activityDaysOneSided(a ottrecidx.ActivityRef, ch Change) []DayDiff {
	order, label, times := activityDayTimes(a)
	var out []DayDiff
	for _, k := range order {
		var lst []string
		for tl, c := range times[k] {
			for range c {
				lst = append(lst, tl)
			}
		}
		sort.Strings(lst)
		dd := DayDiff{Label: label[k]}
		if ch == Added {
			dd.Added = lst
		} else {
			dd.Removed = lst
		}
		out = append(out, dd)
	}
	return out
}

// activityDayTimes buckets an activity's times by day, keyed by weekday (so a
// date-shifted copy aligns) with a human label, returning the encounter order.
func activityDayTimes(a ottrecidx.ActivityRef) (order []string, label map[string]string, times map[string]map[string]int) {
	s := a.Schedule()
	label = map[string]string{}
	times = map[string]map[string]int{}
	for tm := range a.Times() {
		k := dayKey(s, tm)
		if _, ok := times[k]; !ok {
			order = append(order, k)
			times[k] = map[string]int{}
			label[k] = dayLabel(s, tm)
		}
		times[k][timeLabel(tm)]++
	}
	return
}

func multisetDiff(x, y map[string]int) []string {
	var out []string
	for k, xv := range x {
		for range xv - y[k] {
			out = append(out, k)
		}
	}
	return out
}

// signatures builds the similarity signature (set of activity|day|time slots) of
// each schedule.
func signatures(ss []ottrecidx.ScheduleRef) []map[string]int {
	out := make([]map[string]int, len(ss))
	for i, s := range ss {
		out[i] = schedSignature(s)
	}
	return out
}

func schedSignature(s ottrecidx.ScheduleRef) map[string]int {
	sig := map[string]int{}
	for act := range s.Activities() {
		an := actName(act)
		for tm := range act.Times() {
			sig[an+"\x00"+dayKey(s, tm)+"\x00"+timeLabel(tm)]++
		}
	}
	return sig
}

func jaccard(a, b map[string]int) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1
	}
	var inter, union int
	for k, av := range a {
		bv := b[k]
		inter += min(av, bv)
		union += max(av, bv)
	}
	for k, bv := range b {
		if _, ok := a[k]; !ok {
			union += bv
		}
	}
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// dateDistance is the absolute difference in days between the effective start
// dates of two schedules, used as a matching tiebreaker. Open/unknown ranges
// sort last.
func dateDistance(a, b ottrecidx.ScheduleRef) int {
	const big = 1 << 30
	ad, aok := schedStart(a)
	bd, bok := schedStart(b)
	if !aok || !bok {
		return big
	}
	d := int(ad.Sub(bd).Hours() / 24)
	if d < 0 {
		d = -d
	}
	return d
}

func schedStart(s ottrecidx.ScheduleRef) (time.Time, bool) {
	er, ok := s.ComputeEffectiveDateRange()
	if !ok || er.From.IsZero() {
		return time.Time{}, false
	}
	return er.From.GoTime(ottrecidx.TZ)
}

func dayKey(s ottrecidx.ScheduleRef, tm ottrecidx.TimeRef) string {
	if wd, ok := tm.GetWeekday(); ok {
		return wd.String()
	}
	if d, ok := s.GetDayDate(tm.GetScheduleDayIndex()); ok && d.String() != "" {
		return d.String()
	}
	return s.GetDay(tm.GetScheduleDayIndex())
}

func dayLabel(s ottrecidx.ScheduleRef, tm ottrecidx.TimeRef) string {
	if d, ok := s.GetDayDate(tm.GetScheduleDayIndex()); ok && d.String() != "" {
		return d.String()
	}
	if wd, ok := tm.GetWeekday(); ok {
		return wd.String()
	}
	return s.GetDay(tm.GetScheduleDayIndex())
}

func actName(a ottrecidx.ActivityRef) string {
	if s := a.GetName(); s != "" {
		return s
	}
	return a.GetLabel()
}

func schedName(s ottrecidx.ScheduleRef) string {
	if n := s.GetName(); n != "" {
		return n
	}
	return s.GetCaption()
}

func timeLabel(tm ottrecidx.TimeRef) string {
	if r, ok := tm.GetRange(); ok {
		return clockRangeLabel(r)
	}
	return tm.GetLabel()
}

// clockRangeLabel formats a clock range like the website's facility tables.
func clockRangeLabel(r schema.ClockRange) string {
	_, sh, sm := r.Start.Split()
	_, eh, em := r.End.Split()

	sdh := sh % 12
	if sdh == 0 {
		sdh = 12
	}
	edh := eh % 12
	if edh == 0 {
		edh = 12
	}
	esuf := "am"
	if eh >= 12 {
		esuf = "pm"
	}

	var sb strings.Builder
	if (sh < 12) == (eh < 12) {
		writeClock(&sb, sdh, sm, "")
	} else {
		ssuf := "am"
		if sh >= 12 {
			ssuf = "pm"
		}
		writeClock(&sb, sdh, sm, ssuf)
	}
	sb.WriteString("–")
	writeClock(&sb, edh, em, esuf)
	return sb.String()
}

func writeClock(sb *strings.Builder, h, m int, suf string) {
	sb.WriteString(itoa(h))
	sb.WriteByte(':')
	sb.WriteByte('0' + byte(m/10))
	sb.WriteByte('0' + byte(m%10))
	sb.WriteString(suf)
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

// schedDateLabel returns the schedule's effective date range as a human label,
// matching the website (e.g. "Jan 2, 2026 to Mar 4, 2026").
func schedDateLabel(s ottrecidx.ScheduleRef) string {
	er, ok := s.ComputeEffectiveDateRange()
	if !ok {
		return s.GetDate()
	}
	format := func(d schema.Date) string {
		if t, ok := d.GoTime(ottrecidx.TZ); ok {
			return t.Format("Jan 2, 2006")
		}
		return d.String()
	}
	switch {
	case er.From.IsZero() && er.To.IsZero():
		return ""
	case er.From.IsZero():
		return "until " + format(er.To)
	case er.To.IsZero():
		return "from " + format(er.From)
	default:
		return format(er.From) + " to " + format(er.To)
	}
}

// textDiff produces a line-level diff of two scraped HTML fragments.
func textDiff(oldRaw, newRaw string) TextDiff {
	o := strings.TrimSpace(oldRaw)
	n := strings.TrimSpace(newRaw)
	if o == n {
		return TextDiff{Change: Unchanged, Old: o, New: n}
	}
	td := TextDiff{Old: o, New: n}
	switch {
	case o == "":
		td.Change = Added
	case n == "":
		td.Change = Removed
	default:
		td.Change = Modified
	}
	td.Lines = lineDiff(htmlToLines(o), htmlToLines(n))
	return td
}

// htmlToLines extracts visible text lines from a scraped HTML fragment, one per
// block-level element or list item.
func htmlToLines(raw string) []string {
	if raw == "" {
		return nil
	}
	nodes, err := html.ParseFragment(strings.NewReader(raw), &html.Node{
		Type:     html.ElementNode,
		DataAtom: atom.Body,
		Data:     "body",
	})
	if err != nil {
		return []string{collapseSpace(raw)}
	}
	var lines []string
	var cur strings.Builder
	flush := func() {
		if s := collapseSpace(cur.String()); s != "" {
			lines = append(lines, s)
		}
		cur.Reset()
	}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		switch node.Type {
		case html.TextNode:
			cur.WriteString(node.Data)
		case html.ElementNode:
			block := blockElement(node.DataAtom)
			if node.DataAtom == atom.Br {
				flush()
				return
			}
			if block {
				flush()
			}
			for c := node.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			if block {
				flush()
			}
		default:
			for c := node.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
	}
	for _, node := range nodes {
		walk(node)
	}
	flush()
	return lines
}

func blockElement(a atom.Atom) bool {
	switch a {
	case atom.P, atom.Li, atom.Ul, atom.Ol, atom.Div, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Tr, atom.Table, atom.Section, atom.Header, atom.Footer:
		return true
	}
	return false
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// lineDiff computes a line-level diff via the longest common subsequence,
// returning unchanged (context), removed, and added lines in order.
func lineDiff(a, b []string) []TextLine {
	n, m := len(a), len(b)
	// LCS length table.
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []TextLine
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, TextLine{Change: Unchanged, Text: a[i]})
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, TextLine{Change: Removed, Text: a[i]})
			i++
		default:
			out = append(out, TextLine{Change: Added, Text: b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, TextLine{Change: Removed, Text: a[i]})
	}
	for ; j < m; j++ {
		out = append(out, TextLine{Change: Added, Text: b[j]})
	}
	return out
}

// --- change magnitude (for the snapshot range selector) ---

// Magnitudes returns a per-snapshot change score for the given datasets (newest
// first), where result[i] is the number of facilities that changed between
// sets[i] and the older sets[i+1]. The oldest snapshot has no predecessor, so
// its score is 0. This is a cheap approximation of [Compare] used only to size
// the diff page's overview bars, so it is computed from content fingerprints
// rather than a full diff.
func Magnitudes(sets []Dataset) []int {
	out := make([]int, len(sets))
	if len(sets) == 0 {
		return out
	}
	fps := make([]map[string]uint64, len(sets))
	for i := range sets {
		fps[i] = fingerprints(sets[i].Data)
	}
	for i := 0; i+1 < len(sets); i++ {
		out[i] = changedCount(fps[i+1], fps[i])
	}
	return out
}

// MagnitudesFacility returns a per-snapshot change score restricted to the
// facility with the given key (source URL), aligned with sets (newest first):
// result[i] counts the changed schedules and text fields between sets[i] and the
// older sets[i+1]. Used to rescope the diff page's overview bars in "only" mode.
func MagnitudesFacility(sets []Dataset, key string) []int {
	out := make([]int, len(sets))
	for i := 0; i+1 < len(sets); i++ {
		older := findFacility(sets[i+1].Data, key)
		newer := findFacility(sets[i].Data, key)
		if !older.Valid() && !newer.Valid() {
			continue
		}
		out[i] = facilityChangeScore(CompareFacility(older, newer))
	}
	return out
}

// facilityChangeScore is a rough magnitude for a facility diff (number of
// changed schedules, plus group and text changes), 0 if unchanged.
func facilityChangeScore(d FacilityDiff) int {
	if d.Change == Unchanged {
		return 0
	}
	s := 0
	if d.SpecialHours.Change != Unchanged {
		s++
	}
	if d.Notifications.Change != Unchanged {
		s++
	}
	for _, g := range d.Groups {
		s++
		s += len(g.Schedules)
	}
	if s == 0 {
		s = 1
	}
	return s
}

// FacilityStats summarizes a facility's change history within a window of
// snapshots.
type FacilityStats struct {
	Count int       // number of snapshots in which the facility changed
	Last  time.Time // date of the most recent change
}

// FacilityChangeStats returns, for each facility key (source URL), how many times
// it changed and the date of its most recent change within the given datasets
// (newest first): a change is a snapshot whose fingerprint differs from the
// preceding (older) one. Computed from the same cheap fingerprints as
// [Magnitudes].
func FacilityChangeStats(sets []Dataset) map[string]FacilityStats {
	out := map[string]FacilityStats{}
	if len(sets) == 0 {
		return out
	}
	fps := make([]map[string]uint64, len(sets))
	for i := range sets {
		fps[i] = fingerprints(sets[i].Data)
	}
	// scan newest → oldest so the first change seen for a key is the most recent.
	for i := 0; i+1 < len(sets); i++ {
		for k, h := range fps[i] {
			if ph, had := fps[i+1][k]; !had || ph != h {
				st := out[k]
				st.Count++
				if st.Last.IsZero() {
					st.Last = sets[i].Updated
				}
				out[k] = st
			}
		}
	}
	return out
}

func changedCount(old, new map[string]uint64) int {
	var n int
	for k, h := range new {
		if oh, ok := old[k]; !ok || oh != h {
			n++
		}
	}
	for k := range old {
		if _, ok := new[k]; !ok {
			n++
		}
	}
	return n
}

// fingerprints hashes the diff-relevant content of each facility (keyed by
// source URL), so two snapshots can be compared without a full structural diff.
func fingerprints(d ottrecidx.DataRef) map[string]uint64 {
	m := map[string]uint64{}
	if !d.Valid() {
		return m
	}
	for f := range d.Facilities() {
		h := fnv.New64a()
		io.WriteString(h, f.GetSpecialHoursHTML())
		io.WriteString(h, "\x00")
		io.WriteString(h, f.GetNotificationsHTML())
		for g := range f.ScheduleGroups() {
			io.WriteString(h, "\x01")
			io.WriteString(h, groupTitle(g))
			io.WriteString(h, "\x02")
			io.WriteString(h, strings.TrimSpace(g.GetScheduleChangesHTML()))
			for l := range g.GetReservationLinks() {
				io.WriteString(h, "\x07")
				io.WriteString(h, l.URL)
			}
			for s := range g.Schedules() {
				io.WriteString(h, "\x03")
				io.WriteString(h, schedName(s))
				io.WriteString(h, "\x04")
				io.WriteString(h, schedDateLabel(s))
				for a := range s.Activities() {
					io.WriteString(h, "\x05")
					io.WriteString(h, actName(a))
					for tm := range a.Times() {
						io.WriteString(h, "\x06")
						io.WriteString(h, dayKey(s, tm))
						io.WriteString(h, "=")
						io.WriteString(h, timeLabel(tm))
					}
				}
			}
		}
		m[FacilityKey(f)] = h.Sum64()
	}
	return m
}

// --- timeline ---

// TimelineEntry is one transition in a facility's history where it changed.
type TimelineEntry struct {
	Date     time.Time // the (newer) snapshot in which the change appears
	PrevDate time.Time // the preceding snapshot
	Diff     FacilityDiff
}

// FacilityTimeline computes the change history of the facility identified by url
// across the given datasets (which must be newest-first, as returned by
// [Store.Datasets]). Entries are returned newest-first, one per transition in
// which the facility changed.
func FacilityTimeline(sets []Dataset, url string) []TimelineEntry {
	var out []TimelineEntry
	for i := 0; i+1 < len(sets); i++ {
		newer := findFacility(sets[i].Data, url)
		older := findFacility(sets[i+1].Data, url)
		if !newer.Valid() && !older.Valid() {
			continue
		}
		d := CompareFacility(older, newer)
		if d.Change == Unchanged {
			continue
		}
		out = append(out, TimelineEntry{
			Date:     sets[i].Updated,
			PrevDate: sets[i+1].Updated,
			Diff:     d,
		})
	}
	return out
}

// findFacility returns the facility with the given key (source URL) in data, or
// an invalid ref.
func findFacility(data ottrecidx.DataRef, url string) ottrecidx.FacilityRef {
	if !data.Valid() {
		return ottrecidx.FacilityRef{}
	}
	for f := range data.Facilities() {
		if FacilityKey(f) == url {
			return f
		}
	}
	return ottrecidx.FacilityRef{}
}
