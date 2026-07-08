package templates

import (
	"slices"
	"strings"

	"github.com/ottrec/website/pkg/ottrecql"
)

// ScheduleCategory defines a predefined activity category, shared by the
// schedules category pages, the map and today filter pills, and the activities
// directory. Slug/Name/Description/Icon describe the /schedules/{Slug} landing
// page (Icon is a Material Symbols name mapped to a glyph in home.css; the
// glyph must be included in static/fonts.go). Activities holds the activity
// terms, matched by the category pages as ottrecql fuzzy names and by
// [activityCategoryMask] as plain substrings of the normalized activity name.
type ScheduleCategory struct {
	Slug        string
	Name        string
	Description string
	Icon        string
	Activities  []string
}

// ScheduleCategories contains the predefined categories, in display order. An
// activity can be in multiple categories, and ones matching none fall into the
// implicit [categoryOther].
var ScheduleCategories = []ScheduleCategory{
	{"swimming", "Swimming", "All swims, including public, lane, and wave swims.", "pool", []string{"swim"}},
	{"lane-swim", "Lane Swim", "Lane swims.", "pool", []string{"lane swim"}},
	{"aquafit", "Aquafit", "Aquafit and aqua lite.", "water", []string{"aqua"}},
	{"skating", "Skating", "Public, adult, family, senior, and figure skating.", "ice_skating", []string{"skat"}},
	{"hockey", "Hockey", "Hockey, shinny, stick and puck, and ringette.", "sports_hockey", []string{"hockey", "shinny", "puck", "ringette"}},
	{"badminton", "Badminton", "Badminton.", "badminton", []string{"badminton"}},
	{"basketball", "Basketball", "Basketball.", "sports_basketball", []string{"basketball"}},
	{"volleyball", "Volleyball", "Volleyball.", "sports_volleyball", []string{"volleyball"}},
	{"pickleball", "Pickleball", "Pickleball.", "pickleball", []string{"pickleball"}},
	{"squash", "Squash", "Squash.", "sports_tennis", []string{"squash"}},
	{"racquetball", "Racquetball", "Racquetball.", "sports_tennis", []string{"racquetball"}},
}

// categoryOther is the display name of the implicit category for activities
// matching no [ScheduleCategories] entry.
const categoryOther = "Other"

// ScheduleCategoryBySlug resolves a slug from a schedules page path.
func ScheduleCategoryBySlug(slug string) (ScheduleCategory, bool) {
	for _, cat := range ScheduleCategories {
		if cat.Slug == slug {
			return cat, true
		}
	}
	return ScheduleCategory{}, false
}

// Query returns the ottrecql AST matching the category's activities.
func (c ScheduleCategory) Query() ottrecql.Node {
	return &ottrecql.ActivityNode{FuzzyName: c.Activities}
}

// categoryNames returns the category display names with [categoryOther]
// appended, indexed to match [activityCategoryMask] bits. Used as the filter
// keys in the map and today data islands (and thus the shared f-cat URL param).
func categoryNames() []string {
	names := make([]string, 0, len(ScheduleCategories)+1)
	for _, cat := range ScheduleCategories {
		names = append(names, cat.Name)
	}
	return append(names, categoryOther)
}

// activityCategoryMask returns the bitmask of [ScheduleCategories] indexes
// whose Activities terms are substrings of the normalized activity name, or
// the [categoryOther] bit if none match.
func activityCategoryMask(name string) int {
	var cats int
	for c, cat := range ScheduleCategories {
		if slices.ContainsFunc(cat.Activities, func(term string) bool {
			return strings.Contains(name, term) // normalized name is already lowercase with spaces collapsed
		}) {
			cats |= 1 << c
		}
	}
	if cats == 0 {
		cats = 1 << len(ScheduleCategories)
	}
	return cats
}
