// Package ottregions classifies a coordinate into regions of Ottawa.
//
// The places come from OpenStreetMap. The Voyager basemap labels OSM place=*
// nodes through its place_city/place_town/place_villages/place_suburbs style
// layers, so a region here is the nearest such place: an OSM place of class
// city, town, village, or suburb (the four [Class] values). Finer classes
// (hamlet, neighbourhood) are deliberately excluded - the style would draw them
// at high zoom, but the tiles rank-cull them and they don't appear in practice.
//
// The region set is generated from live OSM data.
package ottregions

import (
	"math"

	_ "golang.org/x/image/font/gofont/goregular" // for preview.go
)

//go:generate go run gen.go

// Class is the kind of place a [Region] is. It mirrors the OSM place=* classes
// that the Voyager basemap labels.
type Class uint8

const (
	ClassUnknown Class = iota
	ClassCity
	ClassTown
	ClassVillage
	ClassSuburb
)

// String returns the OSM place=* value for the class (e.g. "suburb").
func (c Class) String() string {
	switch c {
	case ClassCity:
		return "city"
	case ClassTown:
		return "town"
	case ClassVillage:
		return "village"
	case ClassSuburb:
		return "suburb"
	default:
		return "unknown"
	}
}

// Region identifies an Ottawa-area place. The zero value is [RegionUnknown].
// The full set of regions is generated; see [regions_gen.go].
type Region int

// regionInfo is a generated region's metadata (see regions_gen.go).
type regionInfo struct {
	Name     string
	Class    Class
	Lat, Lng float64 // the OSM place node's label point
}

func (r Region) info() regionInfo {
	if r < 0 || int(r) >= len(regions) {
		return regions[RegionUnknown]
	}
	return regions[r]
}

// Regions returns every known region (excluding [RegionUnknown]) in enum order.
func Regions() []Region {
	out := make([]Region, 0, len(regions)-1)
	for r := Region(1); int(r) < len(regions); r++ {
		out = append(out, r)
	}
	return out
}

// Name returns the place's display name (e.g. "Alta Vista"), or "Unknown".
func (r Region) Name() string { return r.info().Name }

// String is an alias for [Region.Name].
func (r Region) String() string { return r.info().Name }

// Class returns the place's class, or [ClassUnknown].
func (r Region) Class() Class { return r.info().Class }

// LatLng returns the place's label point, or (0, 0) for [RegionUnknown].
func (r Region) LatLng() (lat, lng float64) {
	i := r.info()
	return i.Lat, i.Lng
}

// maxRegionDist is how far (in degrees, ~one part in 111 per km) the nearest
// place may be before [RegionAt] gives up and returns [RegionUnknown]. ~20 km
// comfortably covers the gaps between rural villages while rejecting points well
// outside the area.
const maxRegionDist = 20.0 / 111.0

// RegionAt returns the region whose place label point is nearest to the given
// coordinate, or [RegionUnknown] if none is within ~20 km. This is a Voronoi-
// style assignment: OSM place labels are points, not areas, so a coordinate
// belongs to the closest one (longitude weighted by latitude so the comparison
// is in real distance, not raw degrees).
func RegionAt(lat, lng float64) Region {
	best, bestDist := RegionUnknown, math.Inf(1)
	cosLat := math.Cos(lat * math.Pi / 180)
	for r := 1; r < len(regions); r++ { // skip RegionUnknown (index 0)
		dx := (lng - regions[r].Lng) * cosLat
		dy := lat - regions[r].Lat
		if d := dx*dx + dy*dy; d < bestDist {
			best, bestDist = Region(r), d
		}
	}
	if bestDist > maxRegionDist*maxRegionDist {
		return RegionUnknown
	}
	return best
}

// Sector is a coarse part of the city used as a rough hint alongside a
// [Region]. The zero value is [SectorUnknown].
type Sector uint8

const (
	SectorUnknown Sector = iota
	SectorWest
	SectorEast
	SectorCentral
	SectorSouth
)

// String returns the sector's display name (e.g. "West").
func (s Sector) String() string {
	switch s {
	case SectorWest:
		return "West"
	case SectorEast:
		return "East"
	case SectorCentral:
		return "Central"
	case SectorSouth:
		return "South"
	default:
		return "Unknown"
	}
}

// Sector boundaries, hardcoded by eye from the city layout. The checks are
// ordered west, then south, then east, so the far-west suburbs (Kanata,
// Stittsville), which sit at the same latitude as the southern ones (Barrhaven,
// Greely), aren't pulled into South. Everything left over near the core is
// Central.
const (
	sectorWestLng  = -75.78   // west of here is the West end
	sectorSouthLat = 45.35    // south of here (and not West) is the South end
	sectorEastLng  = -75.6064 // east of here (and not West/South) is the East end
)

// SectorAt returns the sector a coordinate falls in. It always returns one of
// West/East/Central/South (never [SectorUnknown]); the boundaries are coarse and
// hardcoded, meant only as a rough orientation hint.
func SectorAt(lat, lng float64) Sector {
	switch {
	case lng < sectorWestLng:
		return SectorWest
	case lat < sectorSouthLat:
		return SectorSouth
	case lng > sectorEastLng:
		return SectorEast
	default:
		return SectorCentral
	}
}
