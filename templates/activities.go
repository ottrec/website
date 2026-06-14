package templates

import (
	"slices"
	"strings"

	"github.com/pgaskin/ottrec-website/pkg/ottrecidx"
	"github.com/pgaskin/ottrec-website/pkg/ottregions"
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
// offering a category of activities, grouped by sector.
type activityCategoryCard struct {
	Name   string
	Groups []activitySectorGroup
}

// activitySectorGroup is the facilities in a card that fall in one sector
// ([ottregions.Sector]), the page's top-level row grouping.
type activitySectorGroup struct {
	Sector     string // "West", "Central", "East", "South", or "Other"
	Facilities []activityCategoryFacility
}

// activityCategoryFacility is a row in a category card. Mask byte d bit p is
// set if the facility offers the category on weekday d (Sunday=0) during
// period p. A facility with activities whose times could not be parsed during
// scraping may have an all-zero mask.
type activityCategoryFacility struct {
	Name   string
	Slug   string // for the facility schedule page link
	Region string // place name shown faint beside the name (may be "")
	Mask   [7]byte
	sector ottregions.Sector // top-level group
}

func (f activityCategoryFacility) Available(day, period int) bool {
	return f.Mask[day]&(1<<period) != 0
}

// activitySectorOrder is the order sector groups appear in (geographic, core
// first); [ottregions.SectorUnknown] facilities go in a trailing "Other" group.
var activitySectorOrder = []ottregions.Sector{
	ottregions.SectorCentral,
	ottregions.SectorWest,
	ottregions.SectorEast,
	ottregions.SectorSouth,
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

	flat := make([][]activityCategoryFacility, len(cards)) // per-card, before grouping
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
		var region string
		if r := fac.Region(); r != ottregions.RegionUnknown {
			region = r.Name()
		}
		for c, m := range masks {
			if m != nil {
				flat[c] = append(flat[c], activityCategoryFacility{
					Name:   fac.GetName(),
					Slug:   slug,
					Region: region,
					Mask:   *m,
					sector: fac.Sector(),
				})
			}
		}
	}

	for c := range cards {
		cards[c].Groups = groupBySector(flat[c])
	}
	return cards
}

// groupBySector buckets facilities into [activitySectorOrder] (then a trailing
// "Other" for those without coordinates), alphabetically within each group.
func groupBySector(facs []activityCategoryFacility) []activitySectorGroup {
	bySector := map[ottregions.Sector][]activityCategoryFacility{}
	for _, f := range facs {
		bySector[f.sector] = append(bySector[f.sector], f)
	}
	var groups []activitySectorGroup
	add := func(s ottregions.Sector, label string) {
		fs := bySector[s]
		if len(fs) == 0 {
			return
		}
		slices.SortFunc(fs, func(a, b activityCategoryFacility) int {
			return strings.Compare(a.Name, b.Name)
		})
		groups = append(groups, activitySectorGroup{Sector: label, Facilities: fs})
	}
	for _, s := range activitySectorOrder {
		add(s, s.String())
	}
	add(ottregions.SectorUnknown, "Other")
	return groups
}
