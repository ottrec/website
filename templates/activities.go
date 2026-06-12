package templates

import (
	"slices"
	"strings"

	"github.com/pgaskin/ottrec-website/pkg/ottrecidx"
)

// activityPeriods contains the time-of-day periods for the activities page as
// inclusive start and exclusive end minutes from midnight.
var activityPeriods = [][2]int{
	{0, 11 * 60},
	{11 * 60, 17 * 60},
	{17 * 60, 24 * 60},
}

var (
	activityPeriodShort = []string{"m", "a", "e"}
	activityPeriodTitle = []string{"morning (until 11:00)", "afternoon (11:00 to 17:00)", "evening (from 17:00)"}
)

// activityCategoryCard is a card on the activities page listing the facilities
// offering a category of activities.
type activityCategoryCard struct {
	Name       string
	Facilities []activityCategoryFacility
}

// activityCategoryFacility is a row in a category card. Mask byte d bit p is
// set if the facility offers the category on weekday d (Sunday=0) during
// period p. A facility with activities whose times could not be parsed during
// scraping may have an all-zero mask.
type activityCategoryFacility struct {
	Name string
	Slug string // for the facility schedule page link
	Mask [7]byte
}

func (f activityCategoryFacility) Available(day, period int) bool {
	return f.Mask[day]&(1<<period) != 0
}

// buildActivityCards collects, for each category from [mapCategories] (plus
// [mapCategoryOther]), the facilities offering it and their availability by
// weekday and period.
func buildActivityCards(data ottrecidx.DataRef) []activityCategoryCard {
	cards := make([]activityCategoryCard, len(mapCategories)+1)
	for c, cat := range mapCategories {
		cards[c].Name = cat.Name
	}
	cards[len(mapCategories)].Name = mapCategoryOther

	slugs := map[string]bool{}
	for fac := range data.Facilities() {
		slug := mapUniqueSlug(slugs, fac.GetName())
		masks := make([]*[7]byte, len(cards))
		for act := range fac.Activities() {
			name := mapActivityName(act)
			if name == "" {
				continue
			}
			cats := mapActivityCategoryMask(name)
			var m [7]byte
			for tm := range act.Times() {
				wd, ok := tm.GetWeekday()
				if !ok {
					continue
				}
				r, ok := tm.GetRange()
				if !ok {
					continue
				}
				maskSetRange(&m, activityPeriods, int(wd), int(r.Start), int(r.End))
			}
			for c := range cards {
				if cats&(1<<c) != 0 {
					if masks[c] == nil {
						masks[c] = new([7]byte)
					}
					for d, b := range m {
						masks[c][d] |= b
					}
				}
			}
		}
		for c, m := range masks {
			if m != nil {
				cards[c].Facilities = append(cards[c].Facilities, activityCategoryFacility{
					Name: fac.GetName(),
					Slug: slug,
					Mask: *m,
				})
			}
		}
	}

	for c := range cards {
		slices.SortFunc(cards[c].Facilities, func(a, b activityCategoryFacility) int {
			return strings.Compare(a.Name, b.Name)
		})
	}
	return cards
}
