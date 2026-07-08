package templates

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ottrec/data-enrichment/enrichidx"
	"github.com/ottrec/scraper/schema"
	"github.com/ottrec/website/pkg/ottrecidx"
	"github.com/ottrec/website/pkg/ottregions"
)

// ActivityLink is an external resource linked from an activity landing page
// (e.g. the City's pool-accessibility or outdoor-rink pages). Cats lists the
// category slugs it applies to; an empty Cats applies to every category.
type ActivityLink struct {
	Label string
	URL   string
	Note  string   // short trailing note (may be "")
	Cats  []string // category slugs this link applies to; empty = all categories
}

// activityLinks are the value-add links, each tagged with the categories it
// applies to (in display order). A category shows every link whose Cats include
// its slug, plus any link with no Cats (which applies to all categories).
var activityLinks = []ActivityLink{
	{
		Label: "Drop-in recreation memberships",
		URL:   "https://ottawa.ca/en/recreation-and-parks/drop-activities/active-ottawa-membership/member-pricing",
		Note:  "from the City of Ottawa",
		Cats:  nil,
	},
	{
		Label: "Pool temperatures and accessibility features",
		URL:   "https://ottawa.ca/en/recreation-and-parks/accessible-features-and-places/accessibility-pools",
		Note:  "from the City of Ottawa",
		Cats:  []string{"swimming", "lane-swim"},
	},
	{
		Label: "Rules and safety information",
		URL:   "https://ottawa.ca/en/recreation-and-parks/drop-activities/descriptions-safety-locations/swimming-aquafitness-and-wave-swim/safety-and-supervision",
		Note:  "from the City of Ottawa",
		Cats:  []string{"swimming", "lane-swim"},
	},
	{
		Label: "Activity information",
		URL:   "https://ottawa.ca/en/recreation-and-parks/drop-activities/descriptions-safety-locations/swimming-aquafitness-and-wave-swim/aquafitness-drop-descriptions",
		Note:  "from the City of Ottawa",
		Cats:  []string{"aquafit"},
	},
	{
		Label: "Outdoor rink locations",
		URL:   "https://ottawa.ca/en/recreation-and-parks/register-courses-and-camps/skating/outdoor-rinks/outdoor-rink-locations",
		Note:  "from the City of Ottawa",
		Cats:  []string{"hockey"},
	},
	{
		Label: "Rules and activity information",
		URL:   "https://ottawa.ca/en/recreation-and-parks/drop-activities/descriptions-safety-locations/skating-and-ice-sports/skating-and-ice-sport-drop-descriptions",
		Note:  "from the City of Ottawa",
		Cats:  []string{"hockey", "skating"},
	},
	{
		Label: "Fees at stand-alone arenas",
		URL:   "https://ottawa.ca/en/recreation-and-parks/drop-activities/descriptions-safety-locations/skating-and-ice-sports/fees-stand-alone-arenas",
		Note:  "from the City of Ottawa",
		Cats:  []string{"hockey", "skating"},
	},
}

// ActivityLinksBySlug returns the value-add links for a category slug: those
// tagged with it, plus any tagged for all categories (empty Cats), in order.
func ActivityLinksBySlug(slug string) []ActivityLink {
	var out []ActivityLink
	for _, l := range activityLinks {
		if len(l.Cats) == 0 || slices.Contains(l.Cats, slug) {
			out = append(out, l)
		}
	}
	return out
}

// WebsiteActivityParams parameterizes the activity landing page at
// /schedules/{slug}.
type WebsiteActivityParams struct {
	Base     string
	Data     ottrecidx.DataRef // full data, for the timestamp and slugger
	Filtered ottrecidx.DataRef // scoped to the category's activities
	Enrich   enrichidx.Ref     // schedule-change enrichment for the today widget (zero = unavailable)
	Cat      ScheduleCategory
	Links    []ActivityLink
}

// activityNotice is one compact notice chip under a today-widget session,
// mirroring the caveats from [todaySessionWarnings]/[todaySessionReservation]
// but condensed. Chips with Href link out (holiday/see-schedule); the rest are
// buttons that open the same /api/{changes,errors,reservations} modal as /today.
type activityNotice struct {
	Label string // short chip text, e.g. "schedule changes"
	Kind  string // severity class: "alert", "warn", "info", "good"
	Icon  string // "warn" (triangle) or "info"
	Warn  string // data-warn value ("changes"/"errors"/"reservations"); "" if Href
	Href  string // external source link; "" for modal chips
	Slug  string
	Group string // schedule group key (see [ScheduleGroupKey])
}

// activitySessionNotices condenses a session's warnings/reservation into compact
// chips, following the same precedence as [todaySessionWarnings].
func activitySessionNotices(s todaySession) []activityNotice {
	var ns []activityNotice
	modal := func(label, kind, icon string) activityNotice {
		return activityNotice{Label: label, Kind: kind, Icon: icon, Warn: "changes", Slug: s.Slug, Group: s.GroupKey}
	}
	if s.EnrichedAdded {
		ns = append(ns, modal("added", "good", "info"))
	}
	if s.EnrichedTimeChanged {
		ns = append(ns, modal("time changed", "good", "info"))
	}
	if s.EnrichedSeeSchedule {
		if s.SourceURL != "" {
			ns = append(ns, activityNotice{Label: "holiday schedule", Kind: "alert", Icon: "warn", Href: s.SourceURL})
		} else {
			ns = append(ns, modal("holiday schedule", "alert", "warn"))
		}
	} else if s.Holiday {
		n := activityNotice{Label: "holiday hours", Kind: "alert", Icon: "warn"}
		if s.SourceURL != "" {
			n.Href = s.SourceURL
		}
		ns = append(ns, n)
	}
	switch {
	case s.EnrichedCancelled:
		ns = append(ns, modal("cancelled", "alert", "warn"))
	case s.EnrichedScopeCancelled:
		ns = append(ns, modal("may be cancelled", "alert", "warn"))
	case s.Changes:
		ns = append(ns, modal("schedule changes", "warn", "warn"))
	case s.EnrichedNotice:
		ns = append(ns, modal("notice", "info", "info"))
	}
	if s.Incomplete {
		n := modal("may be incomplete", "info", "info")
		n.Warn = "errors"
		ns = append(ns, n)
	}
	if s.Reservations {
		label := "reservation?"
		if s.ResvDefinite {
			label = "reserve"
		}
		n := modal(label, "info", "info")
		n.Warn = "reservations"
		ns = append(ns, n)
	}
	return ns
}

// activityTodaySessionCount totals the sessions in a today-widget day.
func activityTodaySessionCount(d todayFeedDay) int {
	n := 0
	for _, hr := range d.Hours {
		n += len(hr.Sessions)
	}
	return n
}

// activitySessionCountLabel labels a session count for the today widget's
// filter banner.
func activitySessionCountLabel(n int) string {
	if n == 1 {
		return "1 session"
	}
	return strconv.Itoa(n) + " sessions"
}

// activityQuotedTerms joins fuzzy match terms into a quoted comma-separated
// list for prose.
func activityQuotedTerms(terms []string) string {
	quoted := make([]string, len(terms))
	for i, t := range terms {
		quoted[i] = "“" + t + "”"
	}
	return strings.Join(quoted, ", ")
}

// activityChangeItem is one posted cancellation or notice surfaced in the
// landing page's changes section, joined to its facility.
type activityChangeItem struct {
	Facility  string
	Slug      string
	GroupKey  string   // schedule group key for /api/changes; "" = facility-scoped
	Dates     []string // date chips like "Sun, Jul 12", or plain words when nothing usable resolved
	DateAmbig bool     // Dates is the raw text as posted, shown with a "?" (not a full interpretation)
	DateNA    bool     // Dates is a plain-words placeholder ("dates unknown" / "no dates found")
	Text      string   // the posted text as written
}

// buildActivityChanges collects the still-relevant posted items for the
// category's facilities from the enrichment: date-associated cancellations in
// one list (chronological), everything else (informational notices, undated
// cancellations, and freeform text that can't be ruled out) in the other
// (dated items first, chronological). Both are empty when enrichment is
// unavailable; slugger must be fresh (consumed per facility, like
// [buildActivityMap]).
func buildActivityChanges(filtered ottrecidx.DataRef, enrich enrichidx.Ref, slugger func(string) string, now time.Time) (cancels, notices []activityChangeItem) {
	if !enrich.OK() {
		return nil, nil
	}
	from := schema.MakeDateFromGo(now.In(ottrecidx.TZ))
	type entry struct {
		it        enrichidx.Item
		fac, slug string
		group     string // group key for the changes modal; "" = facility-scoped
	}
	var all []entry
	seen := map[string]bool{}
	for fac := range filtered.Facilities() {
		name := fac.GetName()
		slug := slugger(name)
		ef := enrich.Facility(name)
		add := func(items []enrichidx.Item, group string) {
			for _, it := range items {
				if !seen[it.ID] {
					seen[it.ID] = true
					all = append(all, entry{it: it, fac: name, slug: slug, group: group})
				}
			}
		}
		add(ef.Items(from), "")
		for grp := range fac.ScheduleGroups() {
			add(ef.Group(grp.GetLabel()).Items(from), ScheduleGroupKey(grp))
		}
	}
	// dated items chronologically, undated ones last (stable: source order)
	slices.SortStableFunc(all, func(a, b entry) int {
		if a.it.Dated != b.it.Dated {
			if a.it.Dated {
				return -1
			}
			return 1
		}
		return int(a.it.Date/10) - int(b.it.Date/10)
	})
	for _, e := range all {
		item := activityChangeItem{
			Facility: e.fac,
			Slug:     e.slug,
			GroupKey: e.group,
			Text:     e.it.Text,
		}
		// prefer an exact reading of the resolved dates; when the span can't
		// be expressed faithfully, show the dates as the facility wrote them
		// (marked with a "?") rather than a partial reading, and otherwise a
		// plain-words placeholder so the absence can't be misread
		switch chips, exact := activityChangeDateChips(e.it, from); {
		case exact:
			item.Dates = chips
		case len(chips) > 0:
			item.Dates, item.DateAmbig = chips, true
		case e.it.Unparsed:
			item.Dates, item.DateNA = []string{"dates unknown"}, true
		default:
			item.Dates, item.DateNA = []string{"no dates found"}, true
		}
		if e.it.Cancelled && e.it.Dated {
			cancels = append(cancels, item)
		} else {
			notices = append(notices, item)
		}
	}
	return cancels, notices
}

// activityChangeDateChips renders the item's resolved dates as chips, and
// whether they're an exact reading. Exact chips: every upcoming posted date
// ("Sun, Jul 12"), or one chip for a range ("Fri, Jul 10 to Mon, Jul 20",
// including one already begun so its start stays visible; "until Mon, Jul 20"
// when only an end was posted; "from Fri, Jul 10"), an open-ended posting
// ("ongoing"), and a weekday restriction combined with any of those
// ("Fridays", "Fridays until Mon, Jul 20", "Fridays, Jul 10 to Aug 29").
//
// A chip must never claim more or less than what actually resolved, so
// anything this can't express faithfully (a partial weekday resolution, an
// unformattable date, an empty result for a span that claimed to resolve)
// falls back to the raw date text as the facility wrote it, returned with
// exact=false; (nil, false) means there's nothing usable at all.
func activityChangeDateChips(it enrichidx.Item, from schema.Date) ([]string, bool) {
	raw := func() ([]string, bool) {
		if it.DateText != "" {
			return []string{it.DateText}, false
		}
		return nil, false
	}
	if !it.Dated || it.WeekdaysPartial {
		return raw()
	}
	f := func(d schema.Date) string {
		t, ok := d.GoTime(ottrecidx.TZ)
		if !ok {
			return ""
		}
		return t.Format("Mon, Jan 2")
	}
	if len(it.Dates) > 0 {
		// explicit dates; the site's warning logic resolves the posting by
		// these alone, so they're the faithful reading even when range or
		// weekday fields coexist
		ds := make([]schema.Date, 0, len(it.Dates))
		for _, d := range it.Dates {
			if int(d)/10 >= int(from)/10 {
				ds = append(ds, d)
			}
		}
		slices.Sort(ds)
		out := make([]string, 0, len(ds))
		for _, d := range ds {
			s := f(d)
			if s == "" {
				return raw()
			}
			out = append(out, s)
		}
		return out, true
	}
	var label string
	switch {
	case it.From != 0 && it.To != 0 && int(it.From)/10 > int(it.To)/10:
		return raw() // a backwards range is a parse gone wrong
	case it.From != 0 && it.To != 0 && int(it.From)/10 == int(it.To)/10:
		label = f(it.From) // a single-day range reads as one date
	case it.From != 0 && it.To != 0:
		if a, b := f(it.From), f(it.To); a != "" && b != "" {
			label = a + " to " + b
		}
	case it.To != 0:
		if b := f(it.To); b != "" {
			label = "until " + b
		}
	case it.From != 0:
		if a := f(it.From); a != "" {
			label = "from " + a
		}
	case it.OpenEnded:
		label = "ongoing"
	}
	if wd := activityChangeWeekdays(it.Weekdays); wd != "" {
		switch {
		case label == "" || label == "ongoing":
			// a bare or open-ended weekday restriction is just the weekdays
			label = wd
		case strings.HasPrefix(label, "until ") || strings.HasPrefix(label, "from "):
			label = wd + " " + label // "Fridays until Mon, Jul 20"
		default:
			label = wd + ", " + label // "Fridays, Jul 10 to Aug 29"
		}
	}
	if label == "" {
		// the span claimed to resolve but nothing was expressible
		return raw()
	}
	return []string{label}, true
}

// activityChangeWeekdays names a weekday restriction ("Fridays", "Saturdays
// and Sundays"), Monday-first. Empty when there is none.
func activityChangeWeekdays(wds []time.Weekday) string {
	if len(wds) == 0 {
		return ""
	}
	sorted := slices.Clone(wds)
	slices.SortFunc(sorted, func(a, b time.Weekday) int {
		return activityMonFirst(int(a)) - activityMonFirst(int(b))
	})
	names := make([]string, len(sorted))
	for i, wd := range sorted {
		names[i] = activityDayName[wd] + "s"
	}
	if len(names) == 1 {
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// activityTodayDay returns the "Today" day from a feed built for the landing
// page's today widget, and whether it was found.
func activityTodayDay(feed todayFeed) (todayFeedDay, bool) {
	for _, d := range feed.Days {
		if d.Rel == "Today" {
			return d, true
		}
	}
	return todayFeedDay{}, false
}

// activityLandingFacility is one facility offering the category, with a concise
// summary of when it does.
type activityLandingFacility struct {
	Name    string
	Slug    string
	Region  string              // finer place name shown faint (may be "")
	When    string              // concise day/period summary (may be "" if unparseable)
	Holiday bool                // also has a holiday schedule (when not in Ranges)
	Ranges  []activityDateRange // set only when the summary differs across date ranges
}

// activityDateRange is one dated context at a facility and the weekday/time
// summary within it, shown only when a facility splits the category across more
// than one. Fixed-date (holiday) schedules collapse to a single entry labelled
// "Holiday schedule" with no summary.
type activityDateRange struct {
	Label   string // date range, or "Holiday schedule"
	Summary string // weekday/time summary (empty for the holiday entry)
}

// activityLandingSector groups facilities by [ottregions.Sector] (the coarse
// west/central/east/south area).
type activityLandingSector struct {
	Sector     string
	Facilities []activityLandingFacility
}

// buildActivityLanding collects, from data already filtered to a category, the
// facilities offering it grouped by sector (each with a when-summary), plus the
// category's normalized activity names sorted most to least common by facility.
func buildActivityLanding(filtered ottrecidx.DataRef, slugger func(string) string) ([]activityLandingSector, []string) {
	bySector := map[ottregions.Sector][]activityLandingFacility{}
	nameCount := map[string]int{}
	for fac := range filtered.Facilities() {
		var m [7]byte                         // overall recurring weekday/period mask
		seenBase := map[string]bool{}         // distinct base names here, for nameCount
		masksByLabel := map[string]*[7]byte{} // recurring times per date-range label
		var labelOrder []string
		hasHoliday := false
		for sch := range fac.Schedules() {
			for act := range sch.Activities() {
				name := mapActivityName(act)
				if name == "" {
					continue
				}
				if base := activityIncludeName(name); base != "" && !seenBase[base] {
					seenBase[base] = true
					nameCount[base]++
				}
			}
			// fixed-date (holiday) schedules collapse to a flat "Holiday schedule"
			// entry rather than a usually-meaningless weekday/time summary
			if sch.LikelyHolidaySchedule() {
				hasHoliday = true
				continue
			}
			label := scheduleDateRangeLabel(sch)
			lm := masksByLabel[label]
			if lm == nil {
				lm = new([7]byte)
				masksByLabel[label] = lm
				labelOrder = append(labelOrder, label)
			}
			for tm := range sch.Times() {
				wd, ok := tm.GetWeekday()
				if !ok {
					continue
				}
				r, ok := tm.GetRange()
				if !ok {
					continue
				}
				maskSetRange(lm, activityPeriods, int(wd), int(r.Start), int(r.End))
				maskSetRange(&m, activityPeriods, int(wd), int(r.Start), int(r.End))
			}
		}
		// list per-date-range summaries only when the recurring schedules genuinely
		// differ across dates; if they all share one summary (or there's just one),
		// the dates add nothing, so collapse to the overall summary
		var labeled []string
		for _, l := range labelOrder {
			if l != "" {
				labeled = append(labeled, l)
			}
		}
		summaries := make(map[string]string, len(labeled))
		distinct := map[string]bool{}
		for _, l := range labeled {
			s := activityWhenSummary(*masksByLabel[l])
			summaries[l] = s
			distinct[s] = true
		}
		when := activityWhenSummary(m)
		var ranges []activityDateRange
		holiday := false
		if len(labeled) >= 2 && len(distinct) >= 2 {
			for _, l := range labeled {
				ranges = append(ranges, activityDateRange{Label: l, Summary: summaries[l]})
			}
			if hasHoliday {
				ranges = append(ranges, activityDateRange{Label: "Holiday schedule"})
			}
		} else {
			holiday = hasHoliday
		}
		if when == "" && len(ranges) == 0 && hasHoliday {
			when = "Holiday schedule"
			holiday = false
		}
		var region string
		if rg := fac.Region(); rg != ottregions.RegionUnknown {
			region = rg.Name()
		}
		sec := fac.Sector()
		bySector[sec] = append(bySector[sec], activityLandingFacility{
			Name:    fac.GetName(),
			Slug:    slugger(fac.GetName()),
			Region:  region,
			When:    when,
			Holiday: holiday,
			Ranges:  ranges,
		})
	}

	var sectors []activityLandingSector
	add := func(s ottregions.Sector, label string) {
		fs := bySector[s]
		if len(fs) == 0 {
			return
		}
		slices.SortFunc(fs, func(a, b activityLandingFacility) int {
			return strings.Compare(a.Name, b.Name)
		})
		sectors = append(sectors, activityLandingSector{Sector: label, Facilities: fs})
	}
	for _, s := range activitySectorOrder {
		add(s, s.String())
	}
	add(ottregions.SectorUnknown, "Other")

	names := make([]string, 0, len(nameCount))
	for n := range nameCount {
		names = append(names, n)
	}
	slices.SortFunc(names, func(a, b string) int {
		if d := nameCount[b] - nameCount[a]; d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})
	return sectors, names
}

// activityMapFacility is a facility marker for the landing page's minimal map.
type activityMapFacility struct {
	Name string  `json:"name"`
	Slug string  `json:"slug"`
	Lat  float32 `json:"lat"`
	Lng  float32 `json:"lng"`
}

// buildActivityMap collects the geolocated facilities offering the category, as
// markers for the minimal map. slugger must be a fresh slugger (it is consumed
// per facility), separate from the one passed to [buildActivityLanding].
func buildActivityMap(filtered ottrecidx.DataRef, slugger func(string) string) []activityMapFacility {
	var facs []activityMapFacility
	for fac := range filtered.Facilities() {
		lng, lat, ok := fac.GetLngLat()
		slug := slugger(fac.GetName()) // consume even without coords, to keep slugs aligned
		if !ok {
			continue
		}
		facs = append(facs, activityMapFacility{
			Name: fac.GetName(),
			Slug: slug,
			Lat:  lat,
			Lng:  lng,
		})
	}
	return facs
}

// activityIncludeName reduces a normalized activity name to its base for the
// "Includes" summary, keeping only the part before any dash or the word
// "court(s)": "public swim - adult" becomes "public swim", "badminton courts"
// becomes "badminton". This collapses variant suffixes so the list dedupes.
func activityIncludeName(name string) string {
	if i := strings.IndexFunc(name, func(r rune) bool { return r == '-' || r == '–' || r == '—' }); i >= 0 {
		name = name[:i]
	}
	if i := strings.Index(name, "court"); i >= 0 {
		name = name[:i]
	}
	return strings.TrimSpace(name)
}

// activityFacilityCount totals the facilities across the landing page's sectors.
func activityFacilityCount(sectors []activityLandingSector) int {
	n := 0
	for _, s := range sectors {
		n += len(s.Facilities)
	}
	return n
}

var activityWhenPeriodWord = []string{"mornings", "afternoons", "evenings"}

// activityWhenSummary renders a [7]byte weekday/period availability mask (byte
// d bit p set = offered on weekday d, period p) as a concise sentence like "Evenings on
// weekdays; mornings and afternoons on weekends" or "All day every day".
// Returns "" for an empty mask.
func activityWhenSummary(mask [7]byte) string {
	byPattern := map[byte][]int{}
	for d := range 7 {
		if mask[d] != 0 {
			byPattern[mask[d]] = append(byPattern[mask[d]], d)
		}
	}
	if len(byPattern) == 0 {
		return ""
	}
	patterns := make([]byte, 0, len(byPattern))
	for b := range byPattern {
		patterns = append(patterns, b)
	}
	// order groups by their earliest day, Monday first
	slices.SortFunc(patterns, func(a, b byte) int {
		return activityMonFirst(byPattern[a][0]) - activityMonFirst(byPattern[b][0])
	})
	parts := make([]string, len(patterns))
	for i, b := range patterns {
		parts[i] = activityWhenPeriods(b) + " " + activityWhenDays(byPattern[b])
	}
	s := strings.Join(parts, "; ")
	return strings.ToUpper(s[:1]) + s[1:]
}

// activityMonFirst maps a weekday (Sunday=0) to a Monday-first index (Monday=0,
// Sunday=6) for ordering.
func activityMonFirst(d int) int {
	return (d + 6) % 7
}

// activityWhenPeriods labels the set periods in a mask byte.
func activityWhenPeriods(b byte) string {
	if b == 0b111 {
		return "throughout the day" // not "all day" since that may be misleading
	}
	var words []string
	for p := range 3 {
		if b&(1<<p) != 0 {
			words = append(words, activityWhenPeriodWord[p])
		}
	}
	switch len(words) {
	case 1:
		return words[0]
	case 2:
		return words[0] + " and " + words[1]
	default:
		return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
	}
}

// activityWhenDays phrases a set of weekdays (Sunday=0) with its preposition:
// all seven days become "every day", Mon-Fri "on weekdays", Sat+Sun "on
// weekends", a single day its plural name, and anything else compressed
// Monday-first runs (e.g. "on Mon to Thu and Sat").
func activityWhenDays(days []int) string {
	var in [7]bool
	for _, d := range days {
		in[d] = true
	}
	if len(days) == 7 {
		return "every day"
	}
	if in[1] && in[2] && in[3] && in[4] && in[5] && !in[0] && !in[6] {
		return "on weekdays"
	}
	if in[0] && in[6] && !in[1] && !in[2] && !in[3] && !in[4] && !in[5] {
		return "on weekends"
	}
	if len(days) == 1 {
		return "on " + activityDayName[days[0]] + "s"
	}
	order := []int{1, 2, 3, 4, 5, 6, 0} // Monday first
	var seq []int
	for _, d := range order {
		if in[d] {
			seq = append(seq, d)
		}
	}
	var parts []string
	for i := 0; i < len(seq); {
		j := i
		for j+1 < len(seq) && activityMonFirst(seq[j+1]) == activityMonFirst(seq[j])+1 {
			j++
		}
		if j > i {
			parts = append(parts, activityDayShort[seq[i]]+" to "+activityDayShort[seq[j]])
		} else {
			parts = append(parts, activityDayShort[seq[i]])
		}
		i = j + 1
	}
	if len(parts) > 1 {
		return "on " + strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	}
	return "on " + parts[0]
}
