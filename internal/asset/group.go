package asset

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"sync"

	"github.com/pgaskin/ottrec-website/internal/httpx"
)

// encodings lists the supported Content-Encodings in negotiation preference
// order ("" is the identity encoding).
var encodings = []string{"", "gzip", "zstd"}

// Group is an [http.Handler] serving a set of assets under a base path. It
// serves each asset at its content-addressed name (redirecting the plain name
// to it) with Accept-Encoding negotiation, caching, and weak-ETag conditional
// requests. Because the Group owns the request-path-to-asset routing table, an
// asset may be served under a different name (or extra path) in each Group.
//
// Assets are built, and the routing table populated, on the first request or
// an explicit [Group.Warm]; their compressed encodings are then warmed in the
// background.
type Group struct {
	base    string
	cache   string
	assets  []*Asset
	aliases map[string]*Asset

	once    sync.Once
	warmErr error
	routes  map[string]route
}

// route is a routing-table entry: an asset to serve, or (if redirect is set) a
// path to redirect to.
type route struct {
	asset    *Asset
	redirect string
}

// NewGroup returns a Group serving assets under base (e.g. "/static/"), each at
// its content-addressed name.
func (s *Set) NewGroup(base string, assets ...*Asset) *Group {
	return &Group{base: base, assets: assets, aliases: map[string]*Asset{}}
}

// Cache sets the Cache-Control header sent with served (content-addressed)
// assets. It returns g for chaining.
func (g *Group) Cache(cacheControl string) *Group {
	g.cache = cacheControl
	return g
}

// Alias serves an additional absolute request path that redirects to a's
// content-addressed URL (e.g. "/favicon.ico"). It returns g for chaining.
func (g *Group) Alias(path string, a *Asset) *Group {
	g.aliases[path] = a
	return g
}

// Path returns the public URL at which a is served by g.
func (g *Group) Path(a *Asset) (string, error) {
	b, err := a.Built()
	if err != nil {
		return "", err
	}
	return g.base + b.Name, nil
}

// Warm builds every asset (concurrently, so compile errors surface here rather
// than on the first request), wires up the routing table, then compresses the
// bodies in the background. It runs at most once; later calls return the same
// result.
func (g *Group) Warm() error {
	g.once.Do(func() { g.warmErr = g.build() })
	return g.warmErr
}

func (g *Group) build() error {
	errs := make([]error, len(g.assets))
	var wg sync.WaitGroup
	for i, a := range g.assets {
		wg.Go(func() { _, errs[i] = a.Built() })
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return err
	}

	g.routes = make(map[string]route, 2*len(g.assets)+len(g.aliases))
	for _, a := range g.assets {
		b, _ := a.Built()
		name := g.base + b.Name
		g.routes[name] = route{asset: a}
		if src := g.base + a.source; src != name {
			g.routes[src] = route{redirect: name}
		}
	}
	for path, a := range g.aliases {
		b, _ := a.Built()
		g.routes[path] = route{redirect: g.base + b.Name}
	}

	// compress in the background; serving builds on demand until ready
	go func() {
		var wg sync.WaitGroup
		for _, a := range g.assets {
			wg.Go(func() { a.Blob() })
		}
		wg.Wait()
	}()
	return nil
}

func (g *Group) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if g.Warm() != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// we support negotiating the content encoding
	w.Header().Add("Vary", "Accept-Encoding")

	rt, ok := g.routes[r.URL.Path]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// redirect plain names to the content-addressed name without caching
	if rt.redirect != "" {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Location", rt.redirect)
		w.WriteHeader(http.StatusTemporaryRedirect)
		return
	}

	b, err := rt.asset.Blob()
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// negotiate the content encoding against what we have available
	offers := make([]string, 0, len(b.Data))
	for _, e := range encodings {
		if _, ok := b.Data[e]; ok {
			offers = append(offers, e)
		}
	}
	encoding := httpx.NegotiateContent(r.Header.Values("Accept-Encoding"), offers)
	if encoding != "" {
		w.Header().Set("Content-Encoding", encoding)
	}

	if b.ContentType != "" {
		w.Header().Set("Content-Type", b.ContentType)
	}
	if g.cache != "" {
		w.Header().Set("Cache-Control", g.cache)
	}

	// weak etag, qualified by the encoding so representations validate
	// independently
	etag := `W/"` + b.Hash
	if encoding != "" {
		etag += "-" + encoding
	}
	etag += `"`
	w.Header().Set("ETag", etag)
	if slices.Contains(r.Header.Values("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	// no body for a head request
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	// serve the content
	body := b.Data[encoding]
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}
