package ottrectm

import (
	"regexp"
	"strings"
	"time"

	"github.com/ottrec/website/pkg/ottrecidx"
	"github.com/ottrec/website/pkg/ottregions"
)

// this file computes high-level per-activity-category statistics across
// snapshots for the time machine trends/summary page. each snapshot represents
// the weekly-recurring schedule in effect on its date, so the natural metric is
// "weekly hours in effect" for a category, plotted across the snapshot timeline.

// Category is a predefined activity category for the trends page, matched
// against the normalized activity names. It mirrors the main site's schedule
// categories (slugs align so the page can borrow their icons).
type Category struct {
	Slug  string
	Name  string
	match *regexp.Regexp
}

// Categories are the predefined activity categories, aligned by index with the
// per-snapshot breakdowns returned by [CategoryStats]. Slugs match the main
// site's schedule category pages.
var Categories = []Category{
	{"swimming", "Swimming", regexp.MustCompile(`swim`)},
	{"lane-swim", "Lane Swim", regexp.MustCompile(`lane swim`)},
	{"aquafit", "Aquafit", regexp.MustCompile(`aqua`)},
	{"skating", "Skating", regexp.MustCompile(`skat`)},
	{"hockey", "Hockey", regexp.MustCompile(`hockey|shinny|stick and puck|ringette`)},
	{"badminton", "Badminton", regexp.MustCompile(`badminton`)},
	{"basketball", "Basketball", regexp.MustCompile(`basketball`)},
	{"volleyball", "Volleyball", regexp.MustCompile(`volleyball`)},
	{"pickleball", "Pickleball", regexp.MustCompile(`pickleball`)},
	{"squash", "Squash", regexp.MustCompile(`squash`)},
	{"racquetball", "Racquetball", regexp.MustCompile(`racquetball`)},
}

// CategoryBySlug returns the category index for a slug, or -1.
func CategoryBySlug(slug string) int {
	for i, c := range Categories {
		if c.Slug == slug {
			return i
		}
	}
	return -1
}

// categoryMask returns the bitmask of [Categories] indexes matching a normalized
// activity name, or 0 if none match.
func categoryMask(name string) uint32 {
	var m uint32
	for c, cat := range Categories {
		if cat.match.MatchString(name) {
			m |= 1 << c
		}
	}
	return m
}

// statPeriods are the time-of-day periods (inclusive start, exclusive end
// minutes from midnight), matching the activities page: morning (<11:00),
// afternoon (11:00–17:00), evening (≥17:00).
var statPeriods = [3][2]int{{0, 11 * 60}, {11 * 60, 17 * 60}, {17 * 60, 24 * 60}}

// PeriodNames are the display names for the [CategoryBreakdown.ByPeriod] indexes.
var PeriodNames = [3]string{"Morning", "Afternoon", "Evening"}

// SectorNames are the display names for the [CategoryBreakdown.BySector]
// indexes, which are [ottregions.Sector] values used directly as the index.
var SectorNames = [5]string{"Other", "West", "East", "Central", "South"}

// workdayStart and workdayEnd bound the typical 9-to-5 workday (minutes from
// midnight, Monday–Friday). Hours outside this window — weekday evenings and
// early mornings, plus all weekend — are counted as "accessible" (reachable by
// someone working a standard workday).
const (
	workdayStart = 9 * 60
	workdayEnd   = 17 * 60
)

// CategoryBreakdown holds the aggregated weekly hours offered for one category
// in one snapshot, broken down several ways. The directional breakdowns
// (ByWeekday, ByPeriod, BySector) each independently sum to Total (modulo
// rounding); Accessible is a subset of Total.
type CategoryBreakdown struct {
	Total      float64    // total weekly hours offered
	Accessible float64    // weekly hours outside the Mon–Fri 9–5 workday (evenings + weekends)
	ByWeekday  [7]float64 // weekly hours by weekday (Sunday=0)
	ByPeriod   [3]float64 // weekly hours by time-of-day period (see statPeriods)
	BySector   [5]float64 // weekly hours by part of the city (indexed by ottregions.Sector)
	Facilities int        // number of facilities offering the category
}

// PerFacility returns the average weekly hours offered per facility (Total over
// Facilities), or 0 if no facility offers the category.
func (bd CategoryBreakdown) PerFacility() float64 {
	if bd.Facilities == 0 {
		return 0
	}
	return bd.Total / float64(bd.Facilities)
}

// walkSegments splits a time slot [start,end) minutes from midnight on weekday
// wd (which may extend past 24:00 for overnight slots) into per-day segments,
// wrapping onto following days, and calls fn for each with the resolved weekday
// and within-day minute range [ds,de).
func walkSegments(wd, start, end int, fn func(day, ds, de int)) {
	for start < end {
		dayStart := (start / 1440) * 1440
		day := (wd + start/1440) % 7
		ds := start - dayStart
		de := min(end-dayStart, 1440)
		fn(day, ds, de)
		start = dayStart + 1440
	}
}

// add accumulates a time slot [start,end) minutes from midnight on weekday wd,
// attributing its duration to the weekday, time-of-day period(s), accessibility
// window, and the facility's sector.
func (bd *CategoryBreakdown) add(sec ottregions.Sector, wd, start, end int) {
	if end <= start {
		return
	}
	total := float64(end-start) / 60
	bd.Total += total
	bd.BySector[sectorIndex(sec)] += total
	walkSegments(wd, start, end, func(day, ds, de int) {
		bd.ByWeekday[day] += float64(de-ds) / 60
		for p, pr := range statPeriods {
			if lo, hi := max(ds, pr[0]), min(de, pr[1]); hi > lo {
				bd.ByPeriod[p] += float64(hi-lo) / 60
			}
		}
		// accessible: the whole segment on weekends, otherwise the part outside
		// the 9–5 workday.
		work := 0
		if day != 0 && day != 6 {
			if lo, hi := max(ds, workdayStart), min(de, workdayEnd); hi > lo {
				work = hi - lo
			}
		}
		bd.Accessible += float64((de-ds)-work) / 60
	})
}

func (bd *CategoryBreakdown) merge(o CategoryBreakdown) {
	bd.Total += o.Total
	bd.Accessible += o.Accessible
	for i := range bd.ByWeekday {
		bd.ByWeekday[i] += o.ByWeekday[i]
	}
	for i := range bd.ByPeriod {
		bd.ByPeriod[i] += o.ByPeriod[i]
	}
	for i := range bd.BySector {
		bd.BySector[i] += o.BySector[i]
	}
}

func sectorIndex(s ottregions.Sector) int {
	if int(s) < len(SectorNames) {
		return int(s)
	}
	return 0
}

// CategoryStats computes the per-category weekly-hours breakdown for each
// snapshot, aligned with sets (newest first). The result is indexed
// [snapshot][category], with category indexes matching [Categories].
func CategoryStats(sets []Dataset) [][]CategoryBreakdown {
	out := make([][]CategoryBreakdown, len(sets))
	for i, ds := range sets {
		out[i] = snapshotCategoryStats(ds.Data, snapshotDate(ds))
	}
	return out
}

// snapshotDate returns the date a snapshot represents (start of its update day
// in the schedule timezone), used to decide which date-ranged schedules are in
// effect.
func snapshotDate(ds Dataset) time.Time {
	t := ds.Updated.In(ottrecidx.TZ)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, ottrecidx.TZ)
}

// snapshotCategoryStats aggregates one snapshot's effective weekly hours per
// category.
func snapshotCategoryStats(data ottrecidx.DataRef, d time.Time) []CategoryBreakdown {
	out := make([]CategoryBreakdown, len(Categories))
	for fac := range data.Facilities() {
		sec := fac.Sector()
		facBD := make([]CategoryBreakdown, len(Categories))
		var contributed uint32
		for act := range effectiveActivities(fac, d) {
			mask := categoryMask(statActName(act))
			if mask == 0 {
				continue
			}
			for tm := range act.Times() {
				wd, ok := tm.GetWeekday()
				if !ok {
					continue
				}
				r, ok := tm.GetRange()
				if !ok || int(r.End) <= int(r.Start) {
					continue
				}
				for c := range Categories {
					if mask&(1<<c) != 0 {
						facBD[c].add(sec, int(wd), int(r.Start), int(r.End))
						contributed |= 1 << c
					}
				}
			}
		}
		for c := range Categories {
			out[c].merge(facBD[c])
			if contributed&(1<<c) != 0 {
				out[c].Facilities++
			}
		}
	}
	return out
}

// statActName returns the normalized activity name (lowercased), falling back to
// the cleaned-up label.
func statActName(act ottrecidx.ActivityRef) string {
	if s := act.GetName(); s != "" {
		return s
	}
	return strings.ToLower(strings.Join(strings.Fields(act.GetLabel()), " "))
}

// effectiveActivities yields the activities actually in effect at the facility
// on date d, resolving date-ranged ("special"/holiday) schedules best-effort:
//
//   - Only schedules effective on d count (open-ended or recurring schedules are
//     always effective; date-ranged ones only when d falls in their range).
//   - Within a group, if a fixed-date schedule is effective on d, it overrides
//     the group's regular schedules for that date (only the fixed one counts).
//   - A fixed-date schedule alone in its own group is treated as a holiday
//     override: its activities suppress the same normalized activities appearing
//     in any other group for that date.
func effectiveActivities(fac ottrecidx.FacilityRef, d time.Time) func(yield func(ottrecidx.ActivityRef) bool) {
	return func(yield func(ottrecidx.ActivityRef) bool) {
		type grp struct {
			scheds []ottrecidx.ScheduleRef
			fixed  []bool // aligned with scheds
			active []bool // aligned with scheds: effective on d
		}
		var groups []grp
		override := map[string]bool{} // normalized activity names overridden by a solo fixed-date group

		for g := range fac.ScheduleGroups() {
			var gr grp
			for s := range g.Schedules() {
				fixed := isFixedDate(s)
				gr.scheds = append(gr.scheds, s)
				gr.fixed = append(gr.fixed, fixed)
				gr.active = append(gr.active, scheduleCoversDate(s, d, fixed))
			}
			// a fixed-date schedule alone in its group overrides matching
			// activities in other groups
			if len(gr.scheds) == 1 && gr.fixed[0] && gr.active[0] {
				for act := range gr.scheds[0].Activities() {
					override[statActName(act)] = true
				}
			}
			groups = append(groups, gr)
		}

		for _, gr := range groups {
			soloFixed := len(gr.scheds) == 1 && gr.fixed[0] && gr.active[0]
			var anyActiveFixed bool
			for i := range gr.scheds {
				if gr.fixed[i] && gr.active[i] {
					anyActiveFixed = true
					break
				}
			}
			for i, s := range gr.scheds {
				if !gr.active[i] {
					continue
				}
				// within a group, an active fixed-date schedule overrides the
				// regular schedules for the date
				if anyActiveFixed && !gr.fixed[i] {
					continue
				}
				for act := range s.Activities() {
					// suppress activities overridden by a solo fixed-date group
					// elsewhere (but not within that group itself)
					if !soloFixed && override[statActName(act)] {
						continue
					}
					if !yield(act) {
						return
					}
				}
			}
		}
	}
}

// HeatmapBins is the number of half-hour columns in a [Dataset.CategoryHeatmap]
// (48 half-hours per day).
const HeatmapBins = 48

// CategoryHeatmap returns a weekday × half-hour grid of the weekly hours offered
// for a category in this snapshot. Cell [weekday][bin] (weekday Sunday=0, bin =
// the half-hour starting at bin*30 minutes from midnight) holds the summed hours
// across all facilities offering the category during that half-hour, so busier
// times (more concurrent availability) read hotter. Uses the same effective-date
// resolution as [CategoryStats].
func (ds Dataset) CategoryHeatmap(category int) [7][HeatmapBins]float64 {
	var m [7][HeatmapBins]float64
	if category < 0 || category >= len(Categories) {
		return m
	}
	d := snapshotDate(ds)
	bit := uint32(1) << category
	for fac := range ds.Data.Facilities() {
		for act := range effectiveActivities(fac, d) {
			if categoryMask(statActName(act))&bit == 0 {
				continue
			}
			for tm := range act.Times() {
				wd, ok := tm.GetWeekday()
				if !ok {
					continue
				}
				r, ok := tm.GetRange()
				if !ok || int(r.End) <= int(r.Start) {
					continue
				}
				walkSegments(int(wd), int(r.Start), int(r.End), func(day, ds, de int) {
					for bin := ds / 30; bin <= (de-1)/30 && bin < HeatmapBins; bin++ {
						if lo, hi := max(ds, bin*30), min(de, (bin+1)*30); hi > lo {
							m[day][bin] += float64(hi-lo) / 60
						}
					}
				})
			}
		}
	}
	return m
}

// scheduleCoversDate reports whether a schedule is in effect on date d. Regular
// (recurring) schedules with no parseable date range are always in effect;
// date-ranged schedules are in effect only when d falls within the range. A
// fixed-date schedule with no parseable range is excluded (it can't be placed).
func scheduleCoversDate(s ottrecidx.ScheduleRef, d time.Time, fixed bool) bool {
	er, ok := s.ComputeEffectiveDateRange()
	if !ok {
		return !fixed
	}
	from, to, ok := er.GoTime(ottrecidx.TZ)
	if !ok {
		return !fixed
	}
	// from is start-of-day, to is end-of-day (inclusive); either may be zero
	// (open). d is start-of-day, so the comparisons are day-inclusive.
	if !from.IsZero() && d.Before(from) {
		return false
	}
	if !to.IsZero() && d.After(to) {
		return false
	}
	return true
}
