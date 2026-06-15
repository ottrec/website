package templates

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pgaskin/ottrec-website/exp/ottrectm"
	"github.com/pgaskin/ottrec-website/pkg/ottrecidx"
)

// This file holds the rendering helpers and page parameters for the ottrec time
// machine (cmd/ottrec-timemachine): a diff between two dataset snapshots and a
// per-facility timeline of changes. The diff itself is computed in
// exp/ottrectm; here we only format it for display.

// TimemachineIndexParams parameterizes the time machine landing page.
type TimemachineIndexParams struct {
	Datasets []ottrectm.Dataset // newest first
}

// TimemachineFacilitiesParams parameterizes the facilities listing page.
type TimemachineFacilitiesParams struct {
	Data  ottrecidx.DataRef                 // newest dataset, for the facility list
	Slugs map[string]string                 // facility key → timeline slug
	Stats map[string]ottrectm.FacilityStats // facility key → change stats
}

// tmFacilityStats returns the facility's change stats and whether it has changed
// at least once in the loaded window.
func tmFacilityStats(params TimemachineFacilitiesParams, fac ottrecidx.FacilityRef) (ottrectm.FacilityStats, bool) {
	s, ok := params.Stats[ottrectm.FacilityKey(fac)]
	return s, ok && s.Count > 0 && !s.Last.IsZero()
}

// tmISO formats a time as an RFC3339 timestamp for a <time datetime> attribute.
func tmISO(t time.Time) string {
	return t.In(ottrecidx.TZ).Format(time.RFC3339)
}

// tmCount formats a change count ("1 time" / "N times").
func tmCount(n int) string {
	if n == 1 {
		return "1 time"
	}
	return strconv.Itoa(n) + " times"
}

// TimemachineDiffParams parameterizes the diff page comparing two datasets.
type TimemachineDiffParams struct {
	Datasets   []ottrectm.Dataset // newest first, for the snapshot pickers
	Magnitudes []int              // per-snapshot change magnitude, aligned with Datasets
	Old, New   ottrectm.Dataset
	Diff       *ottrectm.DataDiff
	Slugs      map[string]string // facility key → timeline slug
	OnlySlug   string            // when set, the diff is restricted to this facility (slug)
	OnlyName   string            // display name of the only-facility
}

// tmBar is one snapshot bar in the diff page's range selector strip.
type tmBar struct {
	ds        ottrectm.Dataset
	prevID    string // id of the immediately older snapshot (for the single-snapshot diff)
	mag       int
	heightPct int  // fill height, 0–100
	inRange   bool // between the selected from/to (inclusive)
	isFrom    bool
	isTo      bool
}

// tmBars builds the range-selector bars for the diff page, oldest first (left to
// right), scaling each bar's fill to the largest change magnitude in the window.
func tmBars(params TimemachineDiffParams) []tmBar {
	var maxMag int
	for _, m := range params.Magnitudes {
		if m > maxMag {
			maxMag = m
		}
	}
	bars := make([]tmBar, 0, len(params.Datasets))
	// Datasets are newest first; reverse so the strip reads oldest → newest.
	for i := len(params.Datasets) - 1; i >= 0; i-- {
		ds := params.Datasets[i]
		var mag int
		if i < len(params.Magnitudes) {
			mag = params.Magnitudes[i]
		}
		h := 0
		if maxMag > 0 {
			// floor so every clickable bar shows even with no change.
			h = 8 + 92*mag/maxMag
		}
		var prevID string
		if i+1 < len(params.Datasets) {
			prevID = params.Datasets[i+1].ID // the immediately older snapshot
		}
		b := tmBar{
			ds:        ds,
			prevID:    prevID,
			mag:       mag,
			heightPct: h,
			isFrom:    ds.ID == params.Old.ID,
			isTo:      ds.ID == params.New.ID,
		}
		t := ds.Updated
		b.inRange = !t.Before(params.Old.Updated) && !t.After(params.New.Updated)
		bars = append(bars, b)
	}
	return bars
}

// tmBarHref returns the no-JS fallback link for clicking a bar: it diffs that
// snapshot against the immediately older one (so a plain click shows what
// changed in that single snapshot). JavaScript upgrades the strip to range
// selection by dragging or tapping two bars.
func tmBarHref(params TimemachineDiffParams, b tmBar) string {
	from := b.prevID
	if from == "" {
		from = b.ds.ID
	}
	return timemachineDiffHref(from, b.ds.ID, params.OnlySlug)
}

// timemachineOnlyHref links to the diff restricted to a single facility.
func timemachineOnlyHref(params TimemachineDiffParams, slug string) string {
	return timemachineDiffHref(params.Old.ID, params.New.ID, slug)
}

// timemachineAllHref links to the diff across all facilities (clearing "only").
func timemachineAllHref(params TimemachineDiffParams) string {
	return timemachineDiffHref(params.Old.ID, params.New.ID, "")
}

// tmPeriodSpan returns the length of the selected comparison window (the time
// between the from and to snapshots).
func tmPeriodSpan(params TimemachineDiffParams) time.Duration {
	return params.New.Updated.Sub(params.Old.Updated)
}

// tmPeriodLabel describes the window length in whole days (e.g. "7 days").
func tmPeriodLabel(params TimemachineDiffParams) string {
	days := int((tmPeriodSpan(params) + 12*time.Hour) / (24 * time.Hour))
	if days == 1 {
		return "1 day"
	}
	return strconv.Itoa(days) + " days"
}

// tmNearestDataset returns the loaded snapshot whose timestamp is closest to
// target.
func tmNearestDataset(datasets []ottrectm.Dataset, target time.Time) ottrectm.Dataset {
	var best ottrectm.Dataset
	var bestD time.Duration
	for i, ds := range datasets {
		d := ds.Updated.Sub(target)
		if d < 0 {
			d = -d
		}
		if i == 0 || d < bestD {
			best, bestD = ds, d
		}
	}
	return best
}

// timemachinePrevHref links to the comparison window one period earlier (its
// newer end abutting the current window's older end). ok is false at the oldest
// edge, where the window cannot move further back.
func timemachinePrevHref(params TimemachineDiffParams) (string, bool) {
	span := tmPeriodSpan(params)
	if span <= 0 {
		return "", false
	}
	newOld := tmNearestDataset(params.Datasets, params.Old.Updated.Add(-span))
	if newOld.ID == params.Old.ID {
		return "", false
	}
	return timemachineDiffHref(newOld.ID, params.Old.ID, params.OnlySlug), true
}

// timemachineNextHref links to the comparison window one period later (its older
// end abutting the current window's newer end). ok is false at the newest edge.
func timemachineNextHref(params TimemachineDiffParams) (string, bool) {
	span := tmPeriodSpan(params)
	if span <= 0 {
		return "", false
	}
	newNew := tmNearestDataset(params.Datasets, params.New.Updated.Add(span))
	if newNew.ID == params.New.ID {
		return "", false
	}
	return timemachineDiffHref(params.New.ID, newNew.ID, params.OnlySlug), true
}

func (b tmBar) Title() string {
	return tmDateShort(b.ds.Updated) + ": " + strconv.Itoa(b.mag) + " changed"
}

// TimemachineFacilityParams parameterizes a facility's change timeline page.
type TimemachineFacilityParams struct {
	Slug     string
	Name     string
	URL      string
	Current  ottrecidx.FacilityRef // newest snapshot of the facility, if it still exists
	Entries  []ottrectm.TimelineEntry
	Datasets []ottrectm.Dataset
}

// TimemachineSlugs builds a facility-key → slug map for linking facility
// timelines, assigning the same unique slugs as [MapFacilityBySlug] within each
// dataset. The first dataset wins for a given facility, so pass the newest
// first; later datasets only contribute facilities the newer ones lack.
func TimemachineSlugs(datasets ...ottrecidx.DataRef) map[string]string {
	out := map[string]string{}
	for _, d := range datasets {
		if !d.Valid() {
			continue
		}
		seen := map[string]bool{}
		for fac := range d.Facilities() {
			s := mapUniqueSlug(seen, fac.GetName())
			k := ottrectm.FacilityKey(fac)
			if _, ok := out[k]; !ok {
				out[k] = s
			}
		}
	}
	return out
}

// TimemachineResolveFacility resolves a facility timeline slug against the loaded
// datasets (newest first), returning the facility's stable key (source URL), its
// name, its City of Ottawa URL, and its newest snapshot ref. The slug uses the
// same scheme as [MapFacilityBySlug] within each dataset, so the newest snapshot
// to contain the facility wins.
func TimemachineResolveFacility(sets []ottrectm.Dataset, slug string) (key, name, url string, current ottrecidx.FacilityRef, ok bool) {
	for _, ds := range sets {
		if fac, found := MapFacilityBySlug(ds.Data, slug); found {
			return ottrectm.FacilityKey(fac), fac.GetName(), fac.GetSourceURL(), fac, true
		}
	}
	return "", "", "", ottrecidx.FacilityRef{}, false
}

// timemachineFacilityHref returns the timeline link for a facility diff, or ""
// if no slug is known.
func timemachineFacilityHref(slugs map[string]string, key string) string {
	if s := slugs[key]; s != "" {
		return "/facility/" + url.PathEscape(s)
	}
	return ""
}

// timemachineDiffHref returns the diff page link for the given snapshot ids,
// optionally restricted to a single facility (only, a slug; "" for all).
func timemachineDiffHref(oldID, newID, only string) string {
	h := "/datasets?from=" + url.QueryEscape(oldID) + "&to=" + url.QueryEscape(newID)
	if only != "" {
		h += "&only=" + url.QueryEscape(only)
	}
	return h
}

// tmDate formats a dataset timestamp for display.
func tmDate(t time.Time) string {
	return t.In(ottrecidx.TZ).Format("Mon, Jan 2, 2006")
}

func tmDateShort(t time.Time) string {
	return t.In(ottrecidx.TZ).Format("2006-01-02")
}

// tmBadgeClass returns the CSS modifier class for a change badge.
func tmBadgeClass(c ottrectm.Change) string {
	switch c {
	case ottrectm.Added:
		return "tm-added"
	case ottrectm.Removed:
		return "tm-removed"
	case ottrectm.Modified:
		return "tm-modified"
	default:
		return ""
	}
}

// tmDayLabel abbreviates a leading weekday name to three letters for compactness
// (e.g. "Monday, Dec 24" → "Mon, Dec 24").
func tmDayLabel(s string) string {
	for wd := time.Sunday; wd <= time.Saturday; wd++ {
		if n := wd.String(); strings.HasPrefix(s, n) {
			return n[:3] + s[len(n):]
		}
	}
	return s
}

// tmCell is one activity×day cell of a schedule diff table.
type tmCell struct {
	Removed []string
	Added   []string
}

// Arrow reports whether the cell is a single time being replaced (one removed,
// one added), returning the old and new labels for an "a → b" display.
func (c tmCell) Arrow() (from, to string, ok bool) {
	if len(c.Removed) == 1 && len(c.Added) == 1 {
		return c.Removed[0], c.Added[0], true
	}
	return "", "", false
}

// tmRow is one activity row of a schedule diff table.
type tmRow struct {
	Change ottrectm.Change
	Name   string
	Cells  []tmCell // aligned with tmTable.Days
}

// tmTable is the activities×days grid of a schedule's changes (only changed
// activities, and only days with a change in some activity).
type tmTable struct {
	Days []string // column labels (display, abbreviated)
	Rows []tmRow
}

// tmScheduleTable arranges a schedule diff's per-activity day changes into a
// compact grid mirroring the site's schedule tables: activities down the side,
// changed days across the top, each cell showing the added (+) and removed (−)
// times for that activity on that day.
func tmScheduleTable(s ottrectm.ScheduleDiff) tmTable {
	var cols []string
	colIdx := map[string]int{}
	for _, a := range s.Activities {
		for _, d := range a.Days {
			if _, ok := colIdx[d.Label]; !ok {
				colIdx[d.Label] = len(cols)
				cols = append(cols, d.Label)
			}
		}
	}
	t := tmTable{}
	for _, c := range cols {
		t.Days = append(t.Days, tmDayLabel(c))
	}
	for _, a := range s.Activities {
		row := tmRow{Change: a.Change, Name: a.Name, Cells: make([]tmCell, len(cols))}
		for _, d := range a.Days {
			row.Cells[colIdx[d.Label]] = tmCell{Removed: d.Removed, Added: d.Added}
		}
		t.Rows = append(t.Rows, row)
	}
	return t
}

// tmScheduleSummary returns a one-line summary of a schedule change, used as the
// schedule's heading.
func tmScheduleSummary(s ottrectm.ScheduleDiff) string {
	name := s.Name
	if name == "" {
		name = "(unnamed schedule)"
	}
	return name
}

// tmDateChange formats a schedule's date-range change.
func tmDateChange(s ottrectm.ScheduleDiff) string {
	switch {
	case s.OldDate == "" && s.NewDate == "":
		return ""
	case s.OldDate == "":
		return s.NewDate
	case s.NewDate == "":
		return s.OldDate + " → (none)"
	case s.OldDate == s.NewDate:
		return s.NewDate
	default:
		return s.OldDate + " → " + s.NewDate
	}
}

// timemachineScheduleFullSummary returns the <summary> text for a collapsed
// full-grid schedule.
func timemachineScheduleFullSummary(s ottrectm.ScheduleDiff) string {
	var b strings.Builder
	if s.Change == ottrectm.Removed {
		b.WriteString("removed schedule")
	} else {
		b.WriteString("new schedule")
	}
	if s.NewDate != "" {
		b.WriteString(": ")
		b.WriteString(s.NewDate)
	} else if s.OldDate != "" {
		b.WriteString(": ")
		b.WriteString(s.OldDate)
	}
	return b.String()
}

// tmChangedTextDiff reports whether a text diff should be shown.
func tmShowText(td ottrectm.TextDiff) bool {
	return td.Change != ottrectm.Unchanged
}

// tmUnchangedClass is the change constant for templ comparisons.
var (
	tmUnchanged = ottrectm.Unchanged
	tmAdded     = ottrectm.Added
	tmRemoved   = ottrectm.Removed
	tmModified  = ottrectm.Modified
)
