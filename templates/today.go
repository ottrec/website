package templates

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ottrec/scraper/schema"
	"github.com/ottrec/website/pkg/ottrecidx"
	"github.com/ottrec/website/pkg/ottregions"
)

// todayWindowDays is how many days the chronological feed runs, including
// today. A week gives a feed that's worth scrolling without being endless; the
// filters do the narrowing.
const todayWindowDays = 7

// todayBadgeWindow is how far (in days) on either side of the feed a fixed-date
// schedule still counts as a "nearby" holiday/special-date for the strong
// badge. Matches the ±1 week the prevalence in PLAN.md was measured against.
const todayBadgeWindow = 7

// timeNow is the wall clock used to anchor "today", overridable in tests.
var timeNow = time.Now

// WebsiteTodayParams parameterizes the "what's on" feed page. The simple filters
// are client-side (the pills in today.ts); the advanced mode is server-side: it
// runs an ottrecql query and builds the feed from the filtered data, replacing
// the pills with the query box (mirroring the schedules advanced search).
type WebsiteTodayParams struct {
	Base       string
	Data       ottrecidx.DataRef // full data, for slugs and the updated timestamp
	Filtered   ottrecidx.DataRef // data to build the feed from (== Data unless advanced)
	Advanced   bool              // advanced (ottrecql) search mode
	Query      string            // current query box contents
	QueryError string            // query parse/limit error to show instead of the feed
}

// todayPeriods are the time-of-day buckets for the time-range filter pill,
// reusing the activities page's morning/afternoon/evening split.
var todayPeriods = activityPeriods

// todaySession is one placed drop-in session in the feed: a single parsed
// clock range for an activity at a facility, on a concrete date. Recurring
// weekday schedules and published fixed-date (holiday) schedules are both
// placed by their own date semantics, independently — we never merge or
// override across them (see PLAN.md "flag, never resolve").
type todaySession struct {
	Start      int    // start minutes from midnight (for sorting/filtering)
	End        int    // end minutes from midnight (may exceed 1440 for overnight)
	Time       string // human clock-range label
	Activity   string
	Facility   string
	Slug       string
	Region     string
	Sector     string // sector display label ("Central"…/"Other")
	Cats       int    // bitmask of [mapCategories] indexes (+ Other bit)
	Weekday    int    // 0 = Sunday, for the weekday filter
	Qual       string // date-range qualifier, shown only for bounded/seasonal schedules
	Fixed      bool   // a published fixed-date (holiday/special) session
	SourceURL  string // City of Ottawa facility page (for warning lines + the source link)
	GroupIndex int    // index of the session's schedule group within the facility (for /api/changes)

	// warning flags (per facility/group), each shown as a warning line under
	// the session opening a modal sourced from /api/changes or
	// /api/holiday-schedules
	Holiday    bool // facility has a fixed-date schedule near the feed
	Changes    bool // the session's group has a schedule-changes block
	Incomplete bool // the facility has scrape errors

	// reservation note (per activity), shown as a grey boxed note below the
	// warnings opening a modal sourced from /api/reservations
	Reservations bool // the activity requires or may require a reservation
	ResvDefinite bool // the requirement is definite (vs. "may be required")
}

// todayHourGroup is the sessions in a day that start within the same clock
// hour, the feed's within-day grouping.
type todayHourGroup struct {
	Hour     int    // 0-23, the start hour
	Label    string // "9 AM", "12 PM"
	Sessions []todaySession
}

// todayFeedDay is one calendar day's worth of sessions in the feed, grouped by
// start hour.
type todayFeedDay struct {
	DateISO string // "2026-06-15"
	Weekday string // "Sunday"
	WdIndex int    // 0 = Sunday
	Month   string // "June 15"
	Rel     string // "Today", "Tomorrow", or ""
	Hours   []todayHourGroup
	Empty   bool // no sessions at all
}

// todayClockLabel formats a clock range like [clockRangeLabel] but elides the
// minutes when they're zero ("9–11am" instead of "9:00–11:00am"), for the
// denser today feed.
func todayClockLabel(r schema.ClockRange) string {
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
	t := func(h, m int, suf string) string {
		if m == 0 {
			return fmt.Sprintf("%d%s", h, suf)
		}
		return fmt.Sprintf("%d:%02d%s", h, m, suf)
	}
	esuf := "am"
	if eh >= 12 {
		esuf = "pm"
	}
	var st string
	if (sh < 12) == (eh < 12) {
		st = t(sdh, sm, "")
	} else {
		ssuf := "am"
		if sh >= 12 {
			ssuf = "pm"
		}
		st = t(sdh, sm, ssuf)
	}
	return st + "–" + t(edh, em, esuf)
}

// todayHourLabel formats an hour (0-23) as a 12-hour label like "9 AM".
func todayHourLabel(h int) string {
	ap := "AM"
	if h >= 12 {
		ap = "PM"
	}
	hh := h % 12
	if hh == 0 {
		hh = 12
	}
	return fmt.Sprintf("%d %s", hh, ap)
}

// todayFeed is the whole server-rendered feed plus the metadata island the
// client needs to drive the filter pills.
type todayFeed struct {
	Days []todayFeedDay
	JSON todayDataJSON
}

// todayDataJSON is embedded into the page as a JSON island and consumed by the
// filter pills in static/today.ts.
type todayDataJSON struct {
	Updated    string              `json:"updated"`
	Weekdays   []string            `json:"weekdays"`   // Sun..Sat
	Categories []string            `json:"categories"` // [mapCategories] + Other
	Periods    []todayPeriodJSON   `json:"periods"`
	Sectors    []string            `json:"sectors"` // group order for the facility pill
	Facilities []todayFacilityJSON `json:"facilities"`
}

type todayPeriodJSON struct {
	Label string `json:"label"`
	Start int    `json:"start"` // minutes from midnight
	End   int    `json:"end"`
}

type todayFacilityJSON struct {
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	Sector string `json:"sector"`
}

// isFixedDate reports whether a schedule's columns are concrete calendar dates
// (a one-off / holiday-week schedule) rather than recurring weekdays. Mirrors
// the heuristic in misc/qc/schedule-date-ambiguity and exp/ottrectm.
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

// FacilityGroupAt returns the i'th schedule group of a facility (document
// order), for resolving the /api/changes group parameter.
func FacilityGroupAt(fac ottrecidx.FacilityRef, i int) (ottrecidx.ScheduleGroupRef, bool) {
	n := 0
	for g := range fac.ScheduleGroups() {
		if n == i {
			return g, true
		}
		n++
	}
	return ottrecidx.ScheduleGroupRef{}, false
}

// todayHasChangesContent reports whether the schedule-changes modal would show
// anything (so the empty state can be rendered otherwise).
func todayHasChangesContent(fac ottrecidx.FacilityRef, grp ottrecidx.ScheduleGroupRef, hasGroup bool) bool {
	if hasGroup && strings.TrimSpace(grp.GetScheduleChangesHTML()) != "" {
		return true
	}
	return strings.TrimSpace(fac.GetNotificationsHTML()) != "" ||
		strings.TrimSpace(fac.GetSpecialHoursHTML()) != ""
}

// todayRangeIntersects reports whether the effective date range er (0 = open
// end) overlaps the inclusive window [from, to], at day granularity.
func todayRangeIntersects(er schema.DateRange, from, to schema.Date) bool {
	if !er.From.IsZero() && int(er.From)/10 > int(to)/10 {
		return false
	}
	if !er.To.IsZero() && int(er.To)/10 < int(from)/10 {
		return false
	}
	return true
}

// todaySectorLabel returns the facility's sector display label, mapping the
// unknown sector to the trailing "Other" group used elsewhere on the site.
func todaySectorLabel(s ottregions.Sector) string {
	if s == ottregions.SectorUnknown {
		return "Other"
	}
	return s.String()
}

// buildTodayFeed places every parseable drop-in session over the next
// [todayWindowDays] days into a single chronological feed, anchored at now (in
// Ottawa time). It is anchored at the wall clock rather than the data date so
// "today" is correct; cached pages may drift but self-correct when the data
// updates daily (as scheduleClass does).
//
// slug assigns each facility its page slug; pass [MapFacilitySlugger] over the
// full (unfiltered) data so slugs stay stable when data is an ottrecql-filtered
// subset (the advanced search), matching the schedules pages.
func buildTodayFeed(data ottrecidx.DataRef, slug func(string) string, now time.Time) todayFeed {
	loc := ottrecidx.TZ
	now = now.In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	// the window dates, and a lookup from a weekday-stripped date to its index
	dates := make([]time.Time, todayWindowDays)
	dayIndex := map[schema.Date]int{}
	for i := range dates {
		d := today.AddDate(0, 0, i)
		dates[i] = d
		dayIndex[schema.MakeDateFromGo(d)/10] = i
	}
	winStart := schema.MakeDateFromGo(today.AddDate(0, 0, -todayBadgeWindow))
	winEnd := schema.MakeDateFromGo(today.AddDate(0, 0, todayWindowDays-1+todayBadgeWindow))

	daySessions := make([][]todaySession, todayWindowDays)

	type facMeta struct {
		slug, region, sector string
	}
	facList := []todayFacilityJSON{}
	facSeen := map[string]bool{}

	for fac := range data.Facilities() {
		meta := facMeta{slug: slug(fac.GetName())}
		if r := fac.Region(); r != ottregions.RegionUnknown {
			meta.region = r.Name()
		}
		meta.sector = todaySectorLabel(fac.Sector())
		sourceURL := fac.GetSourceURL()

		incomplete := hasFacilityErrors(fac)

		// strong (holiday/special-date) badge: the facility has a fixed-date
		// schedule whose dates fall near the feed (or one we can't place, which
		// we can't rule out). Computed once for the whole facility.
		holiday := false
		for s := range fac.Schedules() {
			if !isFixedDate(s) {
				continue
			}
			er, ok := s.ComputeEffectiveDateRange()
			if !ok || todayRangeIntersects(er, winStart, winEnd) {
				holiday = true
				break
			}
		}

		facHasSession := false
		gi := -1
		for grp := range fac.ScheduleGroups() {
			gi++
			changes := grp.GetScheduleChangesHTML() != ""
			for sch := range grp.Schedules() {
				fixed := isFixedDate(sch)
				er, erOK := sch.ComputeEffectiveDateRange()

				// the seasonal/bounded qualifier carried with each session, so
				// the date semantics aren't flattened away. Only shown for
				// bounded recurring schedules (a fixed-date session is already
				// pinned to its day, and plain recurring needs no note).
				var qual string
				if !fixed && erOK && (!er.From.IsZero() || !er.To.IsZero()) {
					qual = scheduleDateRangeLabel(sch)
				}

				for act := range sch.Activities() {
					label := activityLabel(act)
					if label == "" {
						continue
					}
					cats := mapActivityCategoryMask(mapActivityName(act))
					resvReq, resvDef := act.GuessReservationRequirement()
					for tm := range act.Times() {
						r, ok := tm.GetRange()
						if !ok {
							continue // can't place on a timeline
						}
						base := todaySession{
							Start:      int(r.Start),
							End:        int(r.End),
							Time:       todayClockLabel(r),
							Activity:   label,
							Facility:   fac.GetName(),
							Slug:       meta.slug,
							Region:     meta.region,
							Sector:     meta.sector,
							Cats:       cats,
							Qual:       qual,
							Fixed:      fixed,
							SourceURL:  sourceURL,
							GroupIndex: gi,
							Holiday:    holiday,
							Changes:    changes,
							Incomplete: incomplete,

							Reservations: resvReq,
							ResvDefinite: resvDef,
						}

						place := func(i int, wd time.Weekday) {
							s := base
							s.Weekday = int(wd)
							daySessions[i] = append(daySessions[i], s)
							facHasSession = true
						}

						if fixed {
							// published special-date times: pin to the concrete
							// date the column lists.
							if d, ok := tm.SingleDate(); ok {
								if i, in := dayIndex[d/10]; in {
									wd, _ := d.Weekday()
									place(i, wd)
								}
							}
							continue
						}

						// recurring: show on every matching weekday the
						// schedule covers, without suppressing it because some
						// holiday schedule exists.
						wd, ok := tm.GetWeekday()
						if !ok {
							continue
						}
						for i, d := range dates {
							if d.Weekday() != wd {
								continue
							}
							if erOK {
								dd := schema.MakeDateFromGo(d) / 10
								if !er.From.IsZero() && int(er.From)/10 > int(dd) {
									continue
								}
								if !er.To.IsZero() && int(er.To)/10 < int(dd) {
									continue
								}
							}
							place(i, wd)
						}
					}
				}
			}
		}

		if facHasSession && !facSeen[meta.slug] {
			facSeen[meta.slug] = true
			facList = append(facList, todayFacilityJSON{
				Slug:   meta.slug,
				Name:   fac.GetName(),
				Sector: meta.sector,
			})
		}
	}

	// assemble the days, sorting each day's sessions chronologically
	days := make([]todayFeedDay, 0, todayWindowDays)
	for i, d := range dates {
		ss := daySessions[i]
		sort.SliceStable(ss, func(a, b int) bool {
			if ss[a].Start != ss[b].Start {
				return ss[a].Start < ss[b].Start
			}
			if ss[a].Facility != ss[b].Facility {
				return ss[a].Facility < ss[b].Facility
			}
			return ss[a].Activity < ss[b].Activity
		})
		var rel string
		switch i {
		case 0:
			rel = "Today"
		case 1:
			rel = "Tomorrow"
		}
		// group the (already start-sorted) sessions by start hour
		var hours []todayHourGroup
		for _, s := range ss {
			h := s.Start / 60
			if h > 23 {
				h = 23
			}
			if len(hours) == 0 || hours[len(hours)-1].Hour != h {
				hours = append(hours, todayHourGroup{Hour: h, Label: todayHourLabel(h)})
			}
			hours[len(hours)-1].Sessions = append(hours[len(hours)-1].Sessions, s)
		}
		days = append(days, todayFeedDay{
			DateISO: d.Format("2006-01-02"),
			Weekday: d.Weekday().String(),
			WdIndex: int(d.Weekday()),
			Month:   d.Format("January 2"),
			Rel:     rel,
			Hours:   hours,
			Empty:   len(ss) == 0,
		})
	}

	// facility pill options, grouped by sector then alphabetical
	sort.SliceStable(facList, func(a, b int) bool {
		return facList[a].Name < facList[b].Name
	})
	sectors := make([]string, 0, len(activitySectorOrder)+1)
	for _, s := range activitySectorOrder {
		sectors = append(sectors, s.String())
	}
	sectors = append(sectors, "Other")

	catNames := make([]string, 0, len(mapCategories)+1)
	for _, c := range mapCategories {
		catNames = append(catNames, c.Name)
	}
	catNames = append(catNames, mapCategoryOther)

	periods := make([]todayPeriodJSON, len(todayPeriods))
	for i, p := range todayPeriods {
		periods[i] = todayPeriodJSON{Label: activityPeriodLong[i], Start: p[0], End: p[1]}
	}

	feed := todayFeed{
		Days: days,
		JSON: todayDataJSON{
			Updated:    data.Index().Updated().In(loc).Format("2006-01-02"),
			Weekdays:   slices.Clone(mapDays),
			Categories: catNames,
			Periods:    periods,
			Sectors:    sectors,
			Facilities: facList,
		},
	}
	return feed
}
