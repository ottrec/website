// Package static embeds the website's static assets, configures how each is
// compiled (CSS via lightningcss, TypeScript via esbuild), and groups them for
// serving. The asset pipeline and HTTP serving live in internal/asset.
package static

import (
	"bytes"
	"embed"
	"net/http"
	"os"
	"regexp"
	"strconv"

	"github.com/pgaskin/go-lightningcss"
	"github.com/pgaskin/ottrec-website/internal/asset"
	"github.com/pgaskin/ottrec-website/internal/esbuild"
)

//go:generate go run fonts.go
//go:generate go run fetch.go https://cdn.jsdelivr.net/npm/leaflet@1.9.4/dist/leaflet.min.js lib/leaflet.js
//go:generate go run fetch.go https://cdn.jsdelivr.net/npm/leaflet@1.9.4/dist/leaflet.min.css lib/leaflet.css

// Base is the path prefix under which assets are served.
const Base = "/static/"

//go:embed *
var res embed.FS

// assets holds every embedded static asset.
var assets = asset.NewSet(res, mimeType)

// Path returns the public, content-addressed path at which a is served.
func Path(a *asset.Asset) string {
	b, err := a.Built()
	if err != nil {
		panic(err)
	}
	return Base + b.Name
}

// Handler builds and warms g, then returns it as an [http.Handler]. Compile
// failures are fatal: they are deterministic build bugs that should surface at
// startup.
func Handler(g *asset.Group) http.Handler {
	if err := g.Warm(); err != nil {
		panic(err)
	}
	return g
}

func mimeType(ext string) string {
	switch ext {
	case ".woff2":
		return "font/woff2"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".png":
		return "image/png"
	}
	return ""
}

var cssURL = regexp.MustCompile(`url\([^)]+\)`)

// compileCSS minifies a stylesheet with lightningcss and rewrites its url()
// references to the content-addressed names of the assets they point at.
func compileCSS(name string, data []byte, resolve func(string) (string, error)) ([]byte, string, error) {
	if noop, _ := strconv.ParseBool(os.Getenv("DEBUG_POSTCSS_NOOP")); !noop {
		res, err := lightningcss.Transform(data, &lightningcss.Options{
			Filename: name,
			Minify:   true,
			Nesting:  true,
			Targets: lightningcss.Targets{
				Chrome:  lightningcss.Version(110, 0, 0),
				Safari:  lightningcss.Version(15, 0, 0),
				Firefox: lightningcss.Version(110, 0, 0),
			},
		})
		if err != nil {
			return nil, "", err
		}
		data = res.Code
	}
	var rerr error
	out := cssURL.ReplaceAllFunc(data, func(m []byte) []byte {
		ref := string(m[bytes.IndexByte(m, '(')+1 : len(m)-1])
		hashed, err := resolve(ref)
		if err != nil {
			rerr = err
			return m
		}
		return []byte("url(" + hashed + ")")
	})
	if rerr != nil {
		return nil, "", rerr
	}
	return out, ".css", nil
}

// compileTS compiles a TypeScript module to minified JavaScript.
func compileTS(name string, data []byte, _ func(string) (string, error)) ([]byte, string, error) {
	js, err := esbuild.Transform(name, string(data))
	if err != nil {
		return nil, "", err
	}
	return []byte(js), ".js", nil
}

// Assets and the groups they are served in. Top-level stylesheets and scripts
// are compiled; vendored files under a subdirectory are served as-is.
var (
	css = asset.Compile(compileCSS)
	ts  = asset.Compile(compileTS)

	AsapWOFF2         = assets.Register("fonts/asap.woff2")
	SourceSans3WOFF2  = assets.Register("fonts/source_sans_3.woff2")
	SourceSerif4WOFF2 = assets.Register("fonts/source_serif_4.woff2")
	SymbolsWOFF2      = assets.Register("fonts/symbols.woff2")

	LeafletCSS = assets.Register("lib/leaflet.css")
	LeafletJS  = assets.Register("lib/leaflet.js")

	FaviconSVG        = assets.Register("favicon.svg")
	FaviconICO        = assets.Register("favicon.ico")
	AppleTouchIconPNG = assets.Register("apple-touch-icon.png")

	DataCSS       = assets.Register("data.css", css)
	WebsiteCSS    = assets.Register("website.css", css)
	FacilityCSS   = assets.Register("facility.css", css)
	MapCSS        = assets.Register("map.css", css)
	MapJS         = assets.Register("map.ts", ts)
	ActivitiesCSS = assets.Register("activities.css", css)
	ActivitiesJS  = assets.Register("activities.ts", ts)
	SchedulesCSS  = assets.Register("schedules.css", css)
	SchedulesJS   = assets.Register("schedules.ts", ts)
	AboutCSS      = assets.Register("about.css", css)
	HomeCSS       = assets.Register("home.css", css)
	ThemeJS       = assets.Register("theme.ts", ts)
	StarredJS     = assets.Register("starred.ts", ts)
)

var Website = assets.
	NewGroup(Base,
		WebsiteCSS,
		FacilityCSS,
		MapCSS,
		MapJS,
		ActivitiesCSS,
		ActivitiesJS,
		SchedulesCSS,
		SchedulesJS,
		AboutCSS,
		HomeCSS,
		ThemeJS,
		StarredJS,
		SourceSans3WOFF2,
		SourceSerif4WOFF2,
		SymbolsWOFF2,
		AsapWOFF2,
		LeafletCSS,
		LeafletJS,
		FaviconSVG,
		FaviconICO,
		AppleTouchIconPNG,
	).
	Cache("public, max-age=86400").
	Alias("/favicon.ico", FaviconICO)

var Data = assets.
	NewGroup(Base,
		DataCSS,
		SourceSans3WOFF2,
		SourceSerif4WOFF2,
	).
	Cache("public, max-age=86400")
