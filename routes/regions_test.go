package routes

import (
	"bytes"
	"image/png"
	"math"
	"testing"
)

func TestRegionOverlay(t *testing.T) {
	res := regionOverlay()
	if len(res.light.png) == 0 || len(res.dark.png) == 0 {
		t.Fatal("empty overlay png")
	}
	if res.light.etag == res.dark.etag {
		t.Error("light and dark overlays have the same etag")
	}

	wantW := int(math.Round(regLonToX(regEast) - regLonToX(regWest)))
	for name, p := range map[string][]byte{"light": res.light.png, "dark": res.dark.png} {
		img, err := png.Decode(bytes.NewReader(p))
		if err != nil {
			t.Fatalf("%s: decode: %v", name, err)
		}
		if got := img.Bounds().Dx(); got <= 0 || got != wantW {
			t.Errorf("%s: width = %d, want %d", name, got, wantW)
		}
	}
}

func TestRegionColorOverlap(t *testing.T) {
	grid := []int32{1, 2, 3, 4}
	colors := fourColor(grid, 4, 1, 5)
	for i := range 3 {
		if colors[grid[i]] == colors[grid[i+1]] {
			t.Errorf("adjacent regions %d and %d share color %d", grid[i], grid[i+1], colors[grid[i]])
		}
	}
	for _, r := range grid {
		if c := colors[r]; c < 0 || c > 3 {
			t.Errorf("region %d got out-of-range color %d", r, c)
		}
	}
}
