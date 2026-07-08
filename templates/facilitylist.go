package templates

import (
	"slices"
	"strings"

	"github.com/ottrec/website/pkg/ottrecidx"
	"github.com/ottrec/website/pkg/ottregions"
)

type facilityListEntry struct {
	Name   string
	Slug   string
	Region string
}

type facilityListSector struct {
	Sector     string
	Facilities []facilityListEntry
}

// buildFacilitiesList groups every facility by sector (west/central/east/south,
// then a trailing "Other"), alphabetically within each, for the directory.
func buildFacilitiesList(data ottrecidx.DataRef, slugger func(string) string) []facilityListSector {
	bySector := map[ottregions.Sector][]facilityListEntry{}
	for fac := range data.Facilities() {
		var region string
		if rg := fac.Region(); rg != ottregions.RegionUnknown {
			region = rg.Name()
		}
		sec := fac.Sector()
		bySector[sec] = append(bySector[sec], facilityListEntry{
			Name:   fac.GetName(),
			Slug:   slugger(fac.GetName()),
			Region: region,
		})
	}
	var sectors []facilityListSector
	add := func(s ottregions.Sector, label string) {
		fs := bySector[s]
		if len(fs) == 0 {
			return
		}
		slices.SortFunc(fs, func(a, b facilityListEntry) int {
			return strings.Compare(a.Name, b.Name)
		})
		sectors = append(sectors, facilityListSector{Sector: label, Facilities: fs})
	}
	for _, s := range activitySectorOrder {
		add(s, s.String())
	}
	add(ottregions.SectorUnknown, "Other")
	return sectors
}
