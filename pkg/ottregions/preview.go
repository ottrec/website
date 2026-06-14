//go:build ignore

// Command preview renders the sector boundaries (SectorAt) and the region
// Voronoi cells (RegionAt) over the real CartoDB Voyager basemap (the same
// tiles the facility map uses), and prints nothing; it writes sectors.png and
// prints label positions for annotation. Tiles are cached under /tmp/ottrec-
// tiles.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/pgaskin/ottrec-website/pkg/ottregions"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	south, west = 44.8, -76.6
	north, east = 45.7, -75.0
	zoom        = 11
	tileURL     = "https://a.basemaps.cartocdn.com/rastertiles/voyager/%d/%d/%d.png"
	userAgent   = "ottrec-codegen/1.0 (github.com/pgaskin/ottrec)"
	alpha       = 0.42 // overlay opacity over the basemap
)

// cacheDir holds downloaded basemap tiles between runs.
var cacheDir = filepath.Join(os.TempDir(), "ottrec-tiles")

// Web Mercator: world is n() pixels square at this zoom.
func n() float64 { return 256 * math.Exp2(zoom) }

func lonToX(lon float64) float64 { return (lon + 180) / 360 * n() }
func latToY(lat float64) float64 {
	return (1 - math.Asinh(math.Tan(lat*math.Pi/180))/math.Pi) / 2 * n()
}
func xToLon(x float64) float64 { return x/n()*360 - 180 }
func yToLat(y float64) float64 {
	return math.Atan(math.Sinh(math.Pi*(1-2*y/n()))) * 180 / math.Pi
}

// crop rectangle in world pixels
var (
	left  = lonToX(west)
	top   = latToY(north)
	cw    = int(math.Round(lonToX(east) - left))
	ch    = int(math.Round(latToY(south) - top))
	leftI = int(math.Round(left))
	topI  = int(math.Round(top))
)

func px(lat, lng float64) (x, y int) {
	return int(math.Round(lonToX(lng))) - leftI, int(math.Round(latToY(lat))) - topI
}

var fill = map[ottregions.Sector]color.RGBA{
	ottregions.SectorWest:    {40, 120, 255, 255},
	ottregions.SectorEast:    {40, 190, 60, 255},
	ottregions.SectorCentral: {255, 170, 30, 255},
	ottregions.SectorSouth:   {185, 70, 220, 255},
	ottregions.SectorUnknown: {150, 150, 150, 255},
}

func main() {
	base := fetchBase()
	face := newFace(14)

	sectors := clone(base)
	regionsImg := clone(base)
	for py := 0; py < ch; py++ {
		lat := yToLat(top + float64(py))
		for x := 0; x < cw; x++ {
			lng := xToLon(left + float64(x))
			blend(sectors, x, py, fill[ottregions.SectorAt(lat, lng)])
			blend(regionsImg, x, py, regionColor(ottregions.RegionAt(lat, lng)))
		}
	}
	for _, r := range ottregions.Regions() {
		lat, lng := r.LatLng()
		x, y := px(lat, lng)
		for _, img := range []*image.RGBA{sectors, regionsImg} {
			disc(img, x, y, 4, color.RGBA{255, 255, 255, 255})
			disc(img, x, y, 2, color.RGBA{20, 20, 20, 255})
		}
	}
	// label the prominent places (cities, towns, suburbs); villages would clutter
	for _, r := range ottregions.Regions() {
		switch r.Class() {
		case ottregions.ClassCity, ottregions.ClassTown, ottregions.ClassSuburb:
			lat, lng := r.LatLng()
			x, y := px(lat, lng)
			for _, img := range []*image.RGBA{sectors, regionsImg} {
				label(img, face, x+6, y+5, r.Name())
			}
		}
	}
	write(filepath.Join(os.TempDir(), "sectors.png"), sectors)
	write(filepath.Join(os.TempDir(), "regions.png"), regionsImg)
}

func newFace(size float64) font.Face {
	ft, err := opentype.Parse(goregular.TTF)
	if err != nil {
		panic(err)
	}
	face, err := opentype.NewFace(ft, &opentype.FaceOptions{Size: size, DPI: 72})
	if err != nil {
		panic(err)
	}
	return face
}

// label draws text with a white halo (drawn at the 8 neighbouring offsets) so
// it reads over any basemap color. x,y is the baseline origin.
func label(dst *image.RGBA, face font.Face, x, y int, s string) {
	dr := &font.Drawer{Dst: dst, Face: face, Src: image.NewUniform(color.RGBA{255, 255, 255, 255})}
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			dr.Dot = fixed.P(x+dx, y+dy)
			dr.DrawString(s)
		}
	}
	dr.Src = image.NewUniform(color.RGBA{20, 20, 20, 255})
	dr.Dot = fixed.P(x, y)
	dr.DrawString(s)
}

// fetchBase stitches the basemap tiles covering the crop into one image.
func fetchBase() *image.RGBA {
	base := image.NewRGBA(image.Rect(0, 0, cw, ch))
	tx0, tx1 := leftI/256, (leftI+cw)/256
	ty0, ty1 := topI/256, (topI+ch)/256
	for ty := ty0; ty <= ty1; ty++ {
		for tx := tx0; tx <= tx1; tx++ {
			t := tile(tx, ty)
			draw.Draw(base, image.Rect(tx*256-leftI, ty*256-topI, tx*256-leftI+256, ty*256-topI+256), t, image.Point{}, draw.Src)
		}
	}
	return base
}

func tile(x, y int) image.Image {
	p := filepath.Join(cacheDir, fmt.Sprint(zoom), fmt.Sprint(x), fmt.Sprint(y)+".png")
	if f, err := os.Open(p); err == nil {
		img, err := png.Decode(f)
		f.Close()
		if err == nil {
			return img
		}
	}
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf(tileURL, zoom, x, y), nil)
	req.Header.Set("User-Agent", userAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		panic(fmt.Sprintf("tile %d/%d/%d: %s", zoom, x, y, resp.Status))
	}
	img, err := png.Decode(resp.Body)
	if err != nil {
		panic(err)
	}
	os.MkdirAll(filepath.Dir(p), 0o755)
	if f, err := os.Create(p); err == nil {
		png.Encode(f, img)
		f.Close()
	}
	time.Sleep(40 * time.Millisecond) // be polite on cache miss
	return img
}

func blend(img *image.RGBA, x, y int, c color.RGBA) {
	i := img.PixOffset(x, y)
	img.Pix[i+0] = uint8(float64(img.Pix[i+0])*(1-alpha) + float64(c.R)*alpha)
	img.Pix[i+1] = uint8(float64(img.Pix[i+1])*(1-alpha) + float64(c.G)*alpha)
	img.Pix[i+2] = uint8(float64(img.Pix[i+2])*(1-alpha) + float64(c.B)*alpha)
	img.Pix[i+3] = 255
}

func clone(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}

func write(name string, img image.Image) {
	f, err := os.Create(name)
	if err != nil {
		panic(err)
	}
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
	f.Close()
	fmt.Fprintln(os.Stderr, "wrote "+name)
}

// regionColor gives each region a distinct-ish hue (golden-angle spacing so
// neighbouring Voronoi cells differ), and gray for RegionUnknown.
func regionColor(r ottregions.Region) color.RGBA {
	if r == ottregions.RegionUnknown {
		return color.RGBA{150, 150, 150, 255}
	}
	return hsv(math.Mod(float64(r)*137.508, 360), 0.9, 1)
}

func hsv(h, s, v float64) color.RGBA {
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := v - c
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return color.RGBA{uint8((r + m) * 255), uint8((g + m) * 255), uint8((b + m) * 255), 255}
}

func disc(img *image.RGBA, cx, cy, rad int, c color.RGBA) {
	for dy := -rad; dy <= rad; dy++ {
		for dx := -rad; dx <= rad; dx++ {
			if dx*dx+dy*dy <= rad*rad {
				img.Set(cx+dx, cy+dy, c)
			}
		}
	}
}
