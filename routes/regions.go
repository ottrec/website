package routes

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/ottrec/website/internal/httpx"
	"github.com/ottrec/website/pkg/ottregions"
)

// The /about/regions page itself is a markdown-backed about page (see
// templates/about-regions.md and the regions-map/regions-table blocks); this
// file only renders the region shading overlay served at /api/regions/layer.png.

// same as the facility map, web mercator (L.imageOverlay is EPSG:3857)
const (
	regSouth, regWest = 44.8, -76.6
	regNorth, regEast = 45.7, -75.0
	regZoom           = 11 // ~70 m/px over the area, solid colors compress well
)

func regN() float64 {
	return 256 * math.Exp2(regZoom)
}

func regLonToX(l float64) float64 {
	return (l + 180) / 360 * regN()
}

func regLatToY(l float64) float64 {
	return (1 - math.Asinh(math.Tan(l*math.Pi/180))/math.Pi) / 2 * regN()
}

func regXToLon(x float64) float64 {
	return x/regN()*360 - 180
}

func regYToLat(y float64) float64 {
	return math.Atan(math.Sinh(math.Pi*(1-2*y/regN()))) * 180 / math.Pi
}

var regionPalettes = map[bool][4]color.RGBA{
	false: { // light
		{0x43, 0x85, 0xBE, 0xFF}, // blue-400
		{0x87, 0x9A, 0x39, 0xFF}, // green-400
		{0xDA, 0x70, 0x2C, 0xFF}, // orange-400
		{0x8B, 0x7E, 0xC8, 0xFF}, // purple-400
	},
	true: { // dark
		{0x92, 0xBF, 0xDB, 0xFF}, // blue-200
		{0xBE, 0xC9, 0x7E, 0xFF}, // green-200
		{0xF9, 0xAE, 0x77, 0xFF}, // orange-200
		{0xC4, 0xB9, 0xE0, 0xFF}, // purple-200
	},
}

type regionImage struct {
	png  []byte
	etag httpx.ETag
}

type regionOverlayResult struct {
	light, dark regionImage
}

var regionOverlay = sync.OnceValue(func() regionOverlayResult {
	start := time.Now()

	left := regLonToX(regWest)
	top := regLatToY(regNorth)
	cw := int(math.Round(regLonToX(regEast) - left))
	ch := int(math.Round(regLatToY(regSouth) - top))

	// classify every pixel into a region id (0 = unknown/transparent)
	n := len(ottregions.Regions()) + 1 // region ids are 1..n-1
	grid := make([]int32, cw*ch)
	for py := 0; py < ch; py++ {
		lat := regYToLat(top + float64(py))
		row := py * cw
		for x := 0; x < cw; x++ {
			lng := regXToLon(left + float64(x))
			grid[row+x] = int32(ottregions.RegionAt(lat, lng))
		}
	}

	colors := fourColor(grid, cw, ch, n)

	encode := func(dark bool) regionImage {
		pal := regionPalettes[dark]
		img := image.NewRGBA(image.Rect(0, 0, cw, ch))
		for p, r := range grid {
			if r == int32(ottregions.RegionUnknown) {
				continue // transparent
			}
			c := pal[colors[r]]
			i := p * 4
			img.Pix[i+0], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, 0xFF
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			panic(err) // wtf lol
		}
		sum := sha256.Sum256(buf.Bytes())
		return regionImage{png: buf.Bytes(), etag: httpx.MakeETag(hex.EncodeToString(sum[:8]), "")}
	}
	res := regionOverlayResult{light: encode(false), dark: encode(true)}

	slog.Info("rendered region map overlay", slog.Group("size", "light", len(res.light.png), "dark", len(res.dark.png)), "duration", time.Since(start))
	return res
})

func init() {
	go regionOverlay() // compute it at startup
}

// welsh-powell
func fourColor(grid []int32, cw, ch, n int) []int {
	adj := make([]map[int32]struct{}, n)
	mark := func(a, b int32) {
		if a == b || a == 0 || b == 0 {
			return
		}
		if adj[a] == nil {
			adj[a] = map[int32]struct{}{}
		}
		adj[a][b] = struct{}{}
	}
	for py := range ch {
		row := py * cw
		for x := range cw {
			r := grid[row+x]
			if x+1 < cw {
				mark(r, grid[row+x+1])
				mark(grid[row+x+1], r)
			}
			if py+1 < ch {
				mark(r, grid[row+cw+x])
				mark(grid[row+cw+x], r)
			}
		}
	}

	order := make([]int, 0, n-1)
	for r := 1; r < n; r++ {
		order = append(order, r)
	}
	sort.Slice(order, func(i, j int) bool {
		return len(adj[order[i]]) > len(adj[order[j]])
	})

	colors := make([]int, n)
	for i := range colors {
		colors[i] = -1
	}
	for _, r := range order {
		var used [4]bool
		for nb := range adj[r] {
			if c := colors[nb]; c >= 0 {
				used[c] = true
			}
		}
		colors[r] = 0
		for c := range 4 {
			if !used[c] {
				colors[r] = c
				break
			}
		}
	}
	return colors
}

type websiteRegionsLayerHandler struct{}

func (h *websiteRegionsLayerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	res := regionOverlay()
	img := res.light
	if r.URL.Query().Get("theme") == "dark" {
		img = res.dark
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, no-cache")
	if img.etag.Handled(w, r) {
		return
	}
	w.Write(img.png)
}
