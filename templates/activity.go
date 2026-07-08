package templates

import (
	"slices"
	"strings"

	"github.com/ottrec/data-enrichment/enrichidx"
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
	Group int
}

// activitySessionNotices condenses a session's warnings/reservation into compact
// chips, following the same precedence as [todaySessionWarnings].
func activitySessionNotices(s todaySession) []activityNotice {
	var ns []activityNotice
	modal := func(label, kind, icon string) activityNotice {
		return activityNotice{Label: label, Kind: kind, Icon: icon, Warn: "changes", Slug: s.Slug, Group: s.GroupIndex}
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
// weekdays; mornings and afternoons on weekends". Returns "" for an empty mask.
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
		parts[i] = activityWhenPeriods(b) + " on " + activityWhenDays(byPattern[b])
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
		return "throughout the day"
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

// activityWhenDays labels a set of weekdays (Sunday=0), collapsing Mon-Fri to
// "weekdays" and Sat+Sun to "weekends", a single day to its plural name, and
// otherwise compressing Monday-first runs (e.g. "Mo to Th, Sa").
func activityWhenDays(days []int) string {
	var in [7]bool
	for _, d := range days {
		in[d] = true
	}
	if in[1] && in[2] && in[3] && in[4] && in[5] && !in[0] && !in[6] {
		return "weekdays"
	}
	if in[0] && in[6] && !in[1] && !in[2] && !in[3] && !in[4] && !in[5] {
		return "weekends"
	}
	if len(days) == 1 {
		return activityDayName[days[0]] + "s"
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
	return strings.Join(parts, ", ")
}
