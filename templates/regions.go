package templates

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ottrec/website/internal/asset"
	"github.com/ottrec/website/pkg/ottrecidx"
	"github.com/ottrec/website/pkg/ottregions"
	"github.com/ottrec/website/static"
)

// aboutContentBlocks are the dynamic blocks the /about markdown pages can embed
// (see aboutContentBlock). Only the regions page uses them today: an
// interactive map and the centrepoint table.
var aboutContentBlocks = map[string]aboutContentBlock{
	"regions-map": {
		Body: regionsMapBody,
		Foot: regionsMapFoot,
		CSS:  []*asset.Asset{static.LeafletCSS, static.LeafletThemeCSS, static.RegionsCSS},
	},
	"regions-table": {
		Body: regionsTableBody,
	},
	"regions-weight": {
		Body: regionsWeightBody,
	},
}

type regionsData struct {
	Bounds       [4]float64            `json:"bounds"` // [south, west, north, east]
	OverlayLight string                `json:"overlayLight"`
	OverlayDark  string                `json:"overlayDark"`
	Regions      []regionsDataPlace    `json:"regions"`
	Facilities   []regionsDataFacility `json:"facilities"`
	Sectors      regionsDataSectors    `json:"sectors"`
}

type regionsDataPlace struct {
	Name  string  `json:"name"`
	Class string  `json:"class"`
	Lat   float64 `json:"lat"`
	Lng   float64 `json:"lng"`
}

type regionsDataFacility struct {
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lng  float64 `json:"lng"`
}

type regionsDataSectors struct {
	WestLng  float64 `json:"westLng"`
	SouthLat float64 `json:"southLat"`
	EastLng  float64 `json:"eastLng"`
}

func buildRegionsData(data ottrecidx.DataRef) regionsData {
	westLng, southLat, eastLng := ottregions.SectorBoundaries()
	d := regionsData{
		Bounds:       [4]float64{44.8, -76.6, 45.7, -75.0},
		OverlayLight: "/api/regions/layer.png?theme=light",
		OverlayDark:  "/api/regions/layer.png?theme=dark",
		Sectors:      regionsDataSectors{WestLng: westLng, SouthLat: southLat, EastLng: eastLng},
	}
	for _, r := range ottregions.Regions() {
		lat, lng := r.LatLng()
		d.Regions = append(d.Regions, regionsDataPlace{
			Name:  r.Name(),
			Class: r.Class().String(),
			Lat:   lat,
			Lng:   lng,
		})
	}
	for fac := range data.Facilities() {
		lng, lat, ok := fac.GetLngLat()
		if !ok {
			continue
		}
		d.Facilities = append(d.Facilities, regionsDataFacility{
			Name: fac.GetName(),
			Lat:  float64(lat),
			Lng:  float64(lng),
		})
	}
	return d
}

type regionRow struct {
	Name  string
	Class string
	Lat   string
	Lng   string
}

func regionRows() []regionRow {
	regions := ottregions.Regions()
	rows := make([]regionRow, 0, len(regions))
	for _, r := range regions {
		lat, lng := r.LatLng()
		class := r.Class().String()
		rows = append(rows, regionRow{
			Name:  r.Name(),
			Class: strings.ToUpper(class[:1]) + class[1:],
			Lat:   fmt.Sprintf("%.5f", lat),
			Lng:   fmt.Sprintf("%.5f", lng),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows
}

// regionWeightRow is one weighted region in the "regions-weight" table: the
// weight and the facilities its tweak reassigned (see overrides.go).
type regionWeightRow struct {
	Region   string
	Weight   string // signed, e.g. "+0.45 km" / "-0.40 km"
	Affected []regionWeightFacility
}

// regionWeightFacility is a facility a weight reassigned: its name, the region it
// was moved to, and how far past the unweighted boundary it sat (the gap the
// weights had to close). It is listed under the region it was moved from.
type regionWeightFacility struct {
	Name   string
	To     string
	Margin string
}

// regionWeightRows lists the boundary weights and the facilities each one moved,
// computed by comparing the weighted assignment against the unweighted Voronoi.
// Each reassigned facility is listed once, under the region it was moved from
// (the unweighted assignment); the To field gives where it ended up. A grown
// (positive) region that only receives facilities therefore lists none of its
// own, but still appears so every weight is documented.
func regionWeightRows(data ottrecidx.DataRef) []regionWeightRow {
	type aff struct {
		name, to string
		margin   float64
	}
	byRegion := map[ottregions.Region][]aff{}
	for fac := range data.Facilities() {
		lng, lat, ok := fac.GetLngLat()
		if !ok {
			continue
		}
		flat, flng := float64(lat), float64(lng)
		uw := ottregions.RegionAtUnweighted(flat, flng)
		w := ottregions.RegionAt(flat, flng)
		if uw == w {
			continue
		}
		margin := w.DistKm(flat, flng) - uw.DistKm(flat, flng)
		byRegion[uw] = append(byRegion[uw], aff{fac.GetName(), w.Name(), margin})
	}

	var rows []regionWeightRow
	for _, r := range ottregions.Regions() {
		wt := ottregions.RegionWeight(r)
		if wt == 0 {
			continue
		}
		affs := byRegion[r]
		sort.Slice(affs, func(i, j int) bool { return affs[i].name < affs[j].name })
		row := regionWeightRow{Region: r.Name(), Weight: fmt.Sprintf("%+.2f km", wt)}
		for _, a := range affs {
			row.Affected = append(row.Affected, regionWeightFacility{
				Name:   a.name,
				To:     a.to,
				Margin: fmt.Sprintf("%.2f km", a.margin),
			})
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Region < rows[j].Region })
	return rows
}
