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
