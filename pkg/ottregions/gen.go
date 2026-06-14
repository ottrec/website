//go:build ignore

// Command gen fetches the Ottawa-area place labels from OpenStreetMap and
// writes regions.go. Run it with `go generate ./...` (see ottregions.go).
//
// The classification deliberately matches the CartoDB Voyager basemap used on
// the facility map: Voyager labels OSM place=* nodes through its place_city,
// place_town, place_villages, and place_suburbs style layers, so those four
// classes are exactly what we query for. Because the query is by class (not a
// hardcoded list of names), re-running this picks up OSM additions, renames,
// and removals automatically. To change the classification, edit placeClasses
// below.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// placeClasses are the OSM place=* values treated as regions, highest priority
// first (a place tagged with several, e.g. Gatineau is both city and suburb,
// takes the first). These are the classes the Voyager place_* layers label.
var placeClasses = []string{"city", "town", "village", "suburb"}

// bounds is the query area: the greater Ottawa box, matching the map's
// MAX_BOUNDS in static/map.ts. Overpass wants (south, west, north, east).
const (
	south, west = 44.8, -76.6
	north, east = 45.7, -75.0
)

const (
	overpassURL = "https://overpass-api.de/api/interpreter"
	userAgent   = "ottrec-codegen/1.0 (github.com/pgaskin/ottrec)"
	outFile     = "regions.go"
)

func main() {
	log.SetFlags(0)

	elems, err := fetch()
	if err != nil {
		log.Fatalf("fetch: %v", err)
	}
	log.Printf("fetched %d place elements", len(elems))

	regions := collect(elems)
	log.Printf("collected %d regions", len(regions))

	src, err := render(regions)
	if err != nil {
		log.Fatalf("render: %v", err)
	}
	if err := os.WriteFile(outFile, src, 0o644); err != nil {
		log.Fatalf("write: %v", err)
	}
	log.Printf("wrote %s", outFile)
}

// element is a place from the Overpass response.
type element struct {
	Type   string   `json:"type"` // node | way | relation
	Lat    float64  `json:"lat"`  // node
	Lon    float64  `json:"lon"`  // node
	Center struct { // way | relation (from `out center`)
		Lat float64 `json:"lat"`
		Lon float64 `json:"lon"`
	} `json:"center"`
	Tags map[string]string `json:"tags"`
}

func (e element) coord() (lat, lng float64, ok bool) {
	if e.Type == "node" {
		return e.Lat, e.Lon, e.Lat != 0 || e.Lon != 0
	}
	return e.Center.Lat, e.Center.Lon, e.Center.Lat != 0 || e.Center.Lon != 0
}

func fetch() ([]element, error) {
	query := fmt.Sprintf(
		"[out:json][timeout:120];\n"+
			"nwr[\"place\"~\"^(%s)$\"][\"name\"](%g,%g,%g,%g);\n"+
			"out tags center;",
		strings.Join(placeClasses, "|"), south, west, north, east,
	)

	req, err := http.NewRequest(http.MethodPost, overpassURL,
		strings.NewReader(url.Values{"data": {query}}.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("overpass returned %s", resp.Status)
	}

	var body struct {
		Elements []element `json:"elements"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.Elements, nil
}

// region is one collected place.
type region struct {
	name     string
	class    string
	lat, lng float64
	ident    string
}

// collect dedupes the elements by name (a place is often a node plus a boundary
// relation, or tagged with several classes), keeping the highest-priority class
// and preferring a node's point for the label position.
func collect(elems []element) []region {
	prio := map[string]int{}
	for i, c := range placeClasses {
		prio[c] = i
	}

	best := map[string]element{}
	for _, e := range elems {
		name := strings.TrimSpace(e.Tags["name"])
		class := e.Tags["place"]
		if _, ok := prio[class]; !ok {
			continue // not one of our classes
		}
		if name == "" {
			continue
		}
		if _, _, ok := e.coord(); !ok {
			continue
		}
		cur, ok := best[name]
		if !ok {
			best[name] = e
			continue
		}
		// prefer the higher-priority class; on a tie prefer a node (the label
		// anchor) over a relation/way centroid
		switch {
		case prio[class] < prio[cur.Tags["place"]]:
			best[name] = e
		case prio[class] == prio[cur.Tags["place"]] && e.Type == "node" && cur.Type != "node":
			best[name] = e
		}
	}

	regions := make([]region, 0, len(best))
	for name, e := range best {
		lat, lng, _ := e.coord()
		regions = append(regions, region{name: name, class: e.Tags["place"], lat: lat, lng: lng})
	}

	// deterministic order: by name (case-insensitively), then name, then coord
	sort.Slice(regions, func(i, j int) bool {
		li, lj := strings.ToLower(regions[i].name), strings.ToLower(regions[j].name)
		if li != lj {
			return li < lj
		}
		if regions[i].name != regions[j].name {
			return regions[i].name < regions[j].name
		}
		return regions[i].lat < regions[j].lat
	})

	used := map[string]bool{}
	for i := range regions {
		regions[i].ident = uniqueIdent(regions[i].name, used)
	}
	return regions
}

var classConst = map[string]string{
	"city":    "ClassCity",
	"town":    "ClassTown",
	"village": "ClassVillage",
	"suburb":  "ClassSuburb",
}

func render(regions []region) ([]byte, error) {
	var b bytes.Buffer
	fmt.Fprintf(&b, "// Code generated by gen.go from OpenStreetMap data; DO NOT EDIT.\n")
	fmt.Fprintf(&b, "// Generated %s from %s for the area (%g,%g,%g,%g); %d regions.\n\n",
		time.Now().UTC().Format("2006-01-02"), overpassURL, south, west, north, east, len(regions))
	fmt.Fprintf(&b, "package ottregions\n\n")

	fmt.Fprintf(&b, "const (\n")
	fmt.Fprintf(&b, "\tRegionUnknown Region = iota\n")
	for _, r := range regions {
		fmt.Fprintf(&b, "\tRegion%s\n", r.ident)
	}
	fmt.Fprintf(&b, ")\n\n")

	fmt.Fprintf(&b, "// regions holds each region's metadata, indexed by Region. Index 0 is\n")
	fmt.Fprintf(&b, "// RegionUnknown.\n")
	fmt.Fprintf(&b, "var regions = [...]regionInfo{\n")
	fmt.Fprintf(&b, "\tRegionUnknown: {Name: \"Unknown\"},\n")
	for _, r := range regions {
		fmt.Fprintf(&b, "\tRegion%s: {Name: %q, Class: %s, Lat: %s, Lng: %s},\n",
			r.ident, r.name, classConst[r.class], coord(r.lat), coord(r.lng))
	}
	fmt.Fprintf(&b, "}\n")

	return format.Source(b.Bytes())
}

// coord formats a coordinate compactly but losslessly enough for ~metre
// precision (6 decimals ≈ 0.1 m).
func coord(v float64) string {
	return fmt.Sprintf("%.6f", math.Round(v*1e6)/1e6)
}

// uniqueIdent turns a place name into a unique exported Go identifier suffix,
// e.g. "Alta Vista" -> "AltaVista", "Orléans" -> "Orleans".
func uniqueIdent(name string, used map[string]bool) string {
	id := ident(name)
	base := id
	for n := 2; used[id]; n++ {
		id = fmt.Sprintf("%s%d", base, n)
	}
	used[id] = true
	return id
}

func ident(name string) string {
	// strip diacritics (Orléans -> Orleans, Côte -> Cote)
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	if s, _, err := transform.String(t, name); err == nil {
		name = s
	}
	var b strings.Builder
	upNext := true
	for _, r := range name {
		switch {
		case unicode.IsLetter(r):
			if upNext {
				r = unicode.ToUpper(r)
				upNext = false
			}
			b.WriteRune(r)
		case unicode.IsDigit(r):
			b.WriteRune(r)
			upNext = false
		default:
			upNext = true // word break
		}
	}
	id := b.String()
	if id == "" {
		id = "X"
	}
	if r := []rune(id)[0]; unicode.IsDigit(r) {
		id = "_" + id
	}
	return id
}
