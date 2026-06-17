package ottregions

// regionWeights are hand-tuned corrections to the Voronoi region assignment,
// keyed by region name. The region set itself is generated from OpenStreetMap
// (regions.go); these weights layer human judgement on top without editing the
// generated file, so regenerating from OSM stays clean.
//
// A weight is an additive bonus in kilometres on a region's pull: when [RegionAt]
// picks the nearest region, each candidate's distance has its weight subtracted
// before the comparison. A positive weight enlarges the region's cell (it wins
// from farther away); a negative weight shrinks it. Weights only shift the
// boundary between neighbouring regions; they never override the ~20 km
// maxRegionDist cutoff, which is always measured on the true distance.
//
// These are corrections for spots where the bare OSM label anchors put the
// Voronoi boundary in a locally wrong place. Tune them visually with preview.go.
var regionWeights = map[string]float64{
	// South Keys is a small pocket around the transit station, but its OSM label
	// anchor sits far enough north that its raw cell swallows the southern edge
	// of the much larger Alta Vista along the Walkley/Heatherington corridor.
	// Shrinking it by 0.4 km moves the Jim Durrell rec centre (a near-tie, ~50 m
	// closer to the South Keys anchor) and Albion-Heatherington back to Alta
	// Vista, where the ward boundary puts them, while Deborah Anne Kirwan,
	// Greenboro, Sawmill Creek, and Hunt Club-Riverside stay South Keys.
	"South Keys": -0.4,

	// Aylmer is a Gatineau suburb on the Quebec side of the river. Its anchor is
	// the nearest of any place to Pinhey's Point Historic Site (far west, on the
	// Ontario shore), so the bare Voronoi labels that Ottawa site with a Quebec
	// town. Shrink Aylmer so it stops reaching across the river; the site falls
	// to Constance Bay, the nearest Ontario place.
	"Aylmer": -1.5,

	// Lansdowne Park sits right on the Glebe / Old Ottawa South line (a ~30 m
	// difference in anchor distance), and is conventionally part of the Glebe.
	// Nudge Old Ottawa South in just enough to hand Lansdowne to the Glebe
	// without disturbing Brewer, the Ottawa South CC, or Terry Fox.
	"Old Ottawa South": -0.1,

	// The Hintonburg anchor sits between Wellington West and Little Italy and
	// over-reaches south and west: Plant (really Centretown / Little Italy) and
	// Fisher Park (really Westboro / Wellington West) are both closest to it.
	// Pull Hintonburg in so those fall to their grown neighbours below; its own
	// core (Hintonburg CC, Keith Brown, Tom Brown) sits ~0.1-0.7 km from the
	// anchor with the next place ~2.4 km out, so even this much shrink leaves it
	// intact. Plant is closest to Hintonburg by a lot, so the shrink (not the
	// Centretown grow, which Old Town Hall caps) is what carries the boundary.
	"Hintonburg": -0.6,

	// Grow Centretown just enough to claim Plant once Hintonburg is pulled in,
	// without reaching Old Town Hall (Old Ottawa East) or Sandy Hill Arena.
	"Centretown": 0.45,

	// Grow Westboro to claim Fisher Park past the shrunken Hintonburg and past
	// Carlington, without pulling in J.A. Dulude (Carlington).
	"Westboro": 0.6,
}

// regionWeightDeg is regionWeights resolved to a per-Region offset in degrees
// (the unit [RegionAt] works in), aligned with the regions table. Built once at
// init so the hot path is a slice index, not a map lookup. Names in
// regionWeights that match no region are ignored.
var regionWeightDeg = func() []float64 {
	w := make([]float64, len(regions))
	for r := range regions {
		if km, ok := regionWeights[regions[r].Name]; ok {
			w[r] = km / 111.0 // ~111 km per degree
		}
	}
	return w
}()
