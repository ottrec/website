package ottregions

import "testing"

func TestRegionAt(t *testing.T) {
	// each region's own label point must resolve to itself
	for r := Region(1); int(r) < len(regions); r++ {
		lat, lng := r.LatLng()
		if got := RegionAt(lat, lng); got != r {
			t.Errorf("RegionAt(%v label) = %v (%s), want %v (%s)", r, got, got.Class(), r, r.Class())
		}
	}

	// a point well outside the area has no region
	if got := RegionAt(40.0, -80.0); got != RegionUnknown {
		t.Errorf("RegionAt(far away) = %v, want RegionUnknown", got)
	}

	// some examples
	cases := []struct {
		name     string
		lat, lng float64
		want     Region
	}{
		{"Kanata", 45.3128, -75.8940, RegionKanata},
		{"Barrhaven", 45.2802, -75.7597, RegionBarrhaven},
		{"Alta Vista", 45.3865, -75.6625, RegionAltaVista},
	}
	for _, c := range cases {
		if got := RegionAt(c.lat, c.lng); got != c.want {
			t.Errorf("RegionAt(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestRegionOverrides pins the boundary corrections in overrides.go to specific
// facilities, so a later weight tweak or OSM regen can't silently undo them. The
// "must stay" cases are nearby facilities the corrections must not pull away.
func TestRegionOverrides(t *testing.T) {
	cases := []struct {
		name     string
		lat, lng float64
		want     Region
	}{
		// moved by a weight
		{"Jim Durrell Recreation Centre", 45.37293, -75.65943, RegionAltaVista},           // not South Keys
		{"Albion-Heatherington Recreation Centre", 45.37313, -75.648, RegionAltaVista},     // not South Keys
		{"Pinhey's Point Historic Site", 45.44035, -75.95391, RegionConstanceBay},          // not Aylmer (Quebec)
		{"Lansdowne Park", 45.40075, -75.68236, RegionTheGlebe},                            // not Old Ottawa South
		{"Plant Recreation Centre", 45.40784, -75.71481, RegionCentretown},                 // not Hintonburg
		{"Fisher Park Community Centre", 45.39516, -75.73087, RegionWestboro},               // not Hintonburg
		// must stay (corrections must not steal these)
		{"Old Town Hall Community Centre", 45.41291, -75.67999, RegionOldOttawaEast},        // vs grown Centretown
		{"Sandy Hill Arena", 45.41973, -75.67447, RegionSandyHill},                          // vs grown Centretown
		{"J.A. Dulude Arena", 45.37477, -75.74637, RegionCarlington},                        // vs grown Westboro
	}
	for _, c := range cases {
		if got := RegionAt(c.lat, c.lng); got != c.want {
			t.Errorf("RegionAt(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}
