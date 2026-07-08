package templates

import (
	"slices"

	"github.com/ottrec/website/pkg/ottrecidx"
	"github.com/ottrec/website/pkg/ottregions"
)

// buildActivityDirectory returns the categories with landing pages plus the
// distinct normalized activity names not covered by any category (the long
// tail, linked to a schedules search), for the activities directory page.
func buildActivityDirectory(data ottrecidx.DataRef) (cats []ScheduleCategory, others []string) {
	otherBit := 1 << len(mapCategories)
	seenName := map[string]bool{}
	seenBase := map[string]bool{}
	for act := range data.Activities() {
		name := mapActivityName(act)
		if name == "" || seenName[name] {
			continue
		}
		seenName[name] = true
		if mapActivityCategoryMask(name)&^otherBit != 0 {
			continue // covered by a category (has a landing page)
		}
		base := activityIncludeName(name) // trim variant suffixes, like the Includes list
		if base == "" || seenBase[base] {
			continue
		}
		seenBase[base] = true
		others = append(others, base)
	}
	slices.Sort(others)
	return ScheduleCategories, others
}

// activityPeriods contains the time-of-day periods (inclusive start, exclusive
// end minutes from midnight) used to build the weekday/period availability
// masks on the activity landing pages and the today feed's time filter.
var activityPeriods = [][2]int{
	{0, 11 * 60},
	{11 * 60, 17 * 60},
	{17 * 60, 24 * 60},
}

// activityPeriodLong labels the periods (used by the today feed).
var activityPeriodLong = []string{"Morning", "Afternoon", "Evening"}

// activityDayShort/Name are the weekday labels, starting at Sunday, used by the
// landing-page when-summaries.
var (
	activityDayShort = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	activityDayName  = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
)

// activitySectorOrder is the order sector groups appear in (geographic, core
// first); [ottregions.SectorUnknown] facilities go in a trailing "Other" group.
var activitySectorOrder = []ottregions.Sector{
	ottregions.SectorCentral,
	ottregions.SectorWest,
	ottregions.SectorEast,
	ottregions.SectorSouth,
}
