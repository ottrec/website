package templates

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ottrec/website/pkg/ottrecidx"
	"github.com/ottrec/website/pkg/ottregions"
)

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
