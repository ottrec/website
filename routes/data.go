package routes

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"crypto/sha1"
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"weak"

	"github.com/a-h/templ"
	"github.com/klauspost/compress/zip"
	"github.com/pgaskin/ottrec-website/internal/httpx"
	"github.com/pgaskin/ottrec-website/pkg/ottrecdata"
	"github.com/pgaskin/ottrec-website/pkg/ottrecexp"
	"github.com/pgaskin/ottrec-website/pkg/ottrecexph"
	"github.com/pgaskin/ottrec-website/pkg/ottrecidx"
	"github.com/pgaskin/ottrec-website/pkg/ottrecql"
	"github.com/pgaskin/ottrec-website/static"
	"github.com/pgaskin/ottrec-website/templates"
	"github.com/pgaskin/xmlwriter"
)

type DataConfig struct {
	Cache *ottrecdata.Cache
	// HeadHTML is raw HTML injected at the bottom of <head> on the homepage
	// (notably, not the ottrecexph-rendered preview).
	HeadHTML string
}

func Data(cfg DataConfig) (http.Handler, error) {
	if cfg.Cache == nil {
		return nil, fmt.Errorf("no cache specified")
	}
	templates.SetHeadExtra(cfg.HeadHTML)

	mux := http.NewServeMux()

	// TODO: visual low-level historical diff? maybe this should be a separate service?

	mux.Handle("/{$}", &dataHomeHandler{
		Cache:                 cfg.Cache,
		MaxHistoricalVersions: 50,
	})
	mux.Handle("/preview", &dataPreviewHandler{
		Cache:          cfg.Cache,
		MaxQueryLength: 5000,
		MaxQueryCost:   5000,
	})
	mux.Handle("/v1/", &dataAPIv1{
		Base:  "/v1/",
		Cache: cfg.Cache,
	})
	mux.Handle("/export/", &dataExportHandler{
		Base:  "/export/",
		Cache: cfg.Cache,
	})
	mux.Handle("GET /robots.txt", dataRobotsHandler{})
	mux.Handle("GET /sitemap.xml", &dataSitemapHandler{
		Cache: cfg.Cache,
	})
	mux.Handle("/static/", static.Handler(static.Data))

	// so if they panic, they panic early
	dataExportSchemaCSV()
	dataExportSchemaJSON()

	return commonMiddleware(mux), nil
}

type dataHomeHandler struct {
	Host                  string
	Cache                 *ottrecdata.Cache
	MaxHistoricalVersions int
}

func (h *dataHomeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Accept-Encoding")
	w.Header().Set("Cache-Control", "public, no-cache")

	if r.URL.RawQuery != "" {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, r.URL.EscapedPath(), http.StatusTemporaryRedirect)
		return
	}

	latest, _, _, err := h.Cache.ResolveVersion(r.Context(), "latest")
	if err != nil {
		slog.Error("data: failed to resolve latest version", "error", err)
		h.serveError(w, "internal server error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := templates.Render(w, r, templates.DataErrorPage, latest, func() (c templ.Component, status int, err error) {
		versions := slices.Collect(iterLimit(h.Cache.DataVersions(r.Context())(&err), h.MaxHistoricalVersions))
		if err != nil {
			return nil, http.StatusInternalServerError, fmt.Errorf("get data versions: %w", err)
		}
		if len(versions) == 0 {
			return nil, http.StatusServiceUnavailable, fmt.Errorf("data not available, try again later")
		}
		return templates.DataHome(templates.DataHomeParams{
			Canonical: "https://data.ottrec.ca/",
			Latest:    versions[0],
			Versions:  versions,
		}), http.StatusOK, nil
	}); err != nil {
		slog.Error("data: failed to render page", "url", r.URL.String(), "error", err)
	}
}

func (h *dataHomeHandler) serveError(w http.ResponseWriter, message string, code int) {
	d := w.Header()
	d.Set("Content-Length", strconv.Itoa(len(message)+1))
	d.Set("Content-Type", "text/plain; charset=utf-8")
	d.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	io.WriteString(w, message+"\n")
}

type dataRobotsHandler struct{}

func (dataRobotsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body := "User-agent: *\n" +
		"Disallow: /preview\n" +
		"Disallow: /v1/\n" +
		"Disallow: /export/\n" +
		"\n" +
		"Sitemap: https://data.ottrec.ca/sitemap.xml\n"
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, no-cache")
	w.Write([]byte(body))
}

type dataSitemapHandler struct {
	Cache *ottrecdata.Cache
}

func (h *dataSitemapHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, updated, _, err := h.Cache.ResolveVersion(r.Context(), "latest")
	if err != nil {
		slog.Error("data: failed to resolve latest version", "error", err)
		http.Error(w, "internal server error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if id == "" {
		http.Error(w, "data not available, try again later", http.StatusServiceUnavailable)
		return
	}

	buf, err := sitemapXML(updated.In(ottrecidx.TZ).Format("2006-01-02"), []string{
		"https://data.ottrec.ca/",
	})
	if err != nil {
		slog.Error("data: failed to render sitemap", "error", err)
		http.Error(w, "internal server error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	etag := etagWeak(id)
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, no-cache")
	w.Header().Set("ETag", etag)
	if slices.Contains(r.Header.Values("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Write(buf)
}

type dataPreviewHandler struct {
	Cache          *ottrecdata.Cache
	MaxQueryLength int
	MaxQueryCost   int

	mu   sync.Mutex
	blob string
	data ottrecidx.DataRef
}

func (h *dataPreviewHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, no-cache")

	// TODO: historical schedules?

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		h.serveError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var (
		redirect bool
		raw      bool
		all      bool
		q        string
	)
	for k, v := range r.URL.Query() {
		switch k {
		case "raw":
			raw = true
			if len(v) > 0 {
				if b, err := strconv.ParseBool(v[0]); err == nil {
					raw = b
				}
			}
		case "all":
			all = true
			if len(v) > 0 {
				if b, err := strconv.ParseBool(v[0]); err == nil {
					all = b
				}
			}
		case "q":
			if len(v) > 0 {
				q = v[0]
			}
		default:
			redirect = true
		}
	}
	if redirect {
		target := r.URL.EscapedPath()
		sep := "?"
		if raw {
			target += sep + "raw=1"
			sep = "&"
		}
		if all {
			target += sep + "all=1"
			sep = "&"
		}
		if q != "" {
			target += sep + "q=" + url.QueryEscape(q)
		}
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
		return
	}

	ctx := r.Context()

	var timing serverTiming
	var now time.Time

	now = time.Now()
	id, _, _, err := h.Cache.ResolveVersion(ctx, "latest")
	if err != nil {
		slog.Error("preview: failed to resolve latest version", "error", err)
		h.serveError(w, "internal server error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if id == "" {
		h.serveError(w, "no data available, try again later", http.StatusServiceUnavailable)
		return
	}
	timing.Add("resolve", time.Since(now))

	var blobErr error
	var blobHash string
	for hash, format := range h.Cache.DataFormats(ctx, id)(&blobErr) {
		if format == "pb" {
			blobHash = hash
			break
		}
	}
	if blobErr != nil {
		slog.Error("preview: failed to resolve formats", "id", id, "error", blobErr)
		h.serveError(w, "internal server error: "+blobErr.Error(), http.StatusInternalServerError)
		return
	}
	if blobHash == "" {
		slog.Error("preview: no pb blob found", "id", id)
		h.serveError(w, "internal server error: no pb blob", http.StatusInternalServerError)
		return
	}

	hw := sha1.New()
	io.WriteString(hw, blobHash)
	io.WriteString(hw, exehash)
	if raw {
		hw.Write([]byte{1})
	}
	if all {
		hw.Write([]byte{2})
	}
	io.WriteString(hw, q)
	etag := `"` + base32.StdEncoding.EncodeToString(hw.Sum(nil)) + `"`

	if slices.Contains(r.Header.Values("If-None-Match"), etag) {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}

	h.mu.Lock()
	cachedHash, data := h.blob, h.data
	h.mu.Unlock()

	if cachedHash != blobHash {
		now = time.Now()
		var pb []byte
		exists, err := h.Cache.ReadBlob(ctx, blobHash, false, func(r io.Reader, size int64) error {
			pb = make([]byte, size)
			_, err := io.ReadFull(r, pb)
			return err
		})
		if err != nil {
			slog.Error("preview: failed to read blob", "hash", blobHash, "error", err)
			h.serveError(w, "internal server error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if !exists {
			slog.Error("preview: missing blob", "hash", blobHash)
			h.serveError(w, "internal server error: missing blob", http.StatusInternalServerError)
			return
		}
		timing.Add("read", time.Since(now))

		now = time.Now()
		idx, err := new(ottrecidx.Indexer).Load(pb)
		if err != nil {
			slog.Error("preview: failed to load index", "id", id, "error", err)
			h.serveError(w, "internal server error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		timing.Add("load", time.Since(now))

		data = idx.Data()
		h.mu.Lock()
		h.blob, h.data = blobHash, data
		h.mu.Unlock()
	}
	if q != "" {
		if h.MaxQueryLength > 0 && len(q) > h.MaxQueryLength {
			h.serveError(w, fmt.Sprintf("query too long (%d > %d)", len(q), h.MaxQueryLength), http.StatusRequestEntityTooLarge)
			return
		}

		now = time.Now()
		ast, err := ottrecql.Parse(q)
		timing.Add("query-parse", time.Since(now))
		if err != nil {
			h.serveError(w, "query: "+err.Error(), http.StatusBadRequest)
			return
		}

		now = time.Now()
		ast = ottrecql.Optimize(ast)
		timing.Add("query-optimize", time.Since(now))

		if h.MaxQueryCost > 0 {
			if cost := ottrecql.Cost(ast); cost > h.MaxQueryCost {
				h.serveError(w, fmt.Sprintf("query too complex (%d > %d)", cost, h.MaxQueryCost), http.StatusRequestEntityTooLarge)
				return
			}
		}

		now = time.Now()
		expr, err := ottrecql.Compile(ast, nil)
		timing.Add("query-compile", time.Since(now))
		if err != nil {
			h.serveError(w, "query: "+err.Error(), http.StatusBadRequest)
			return
		}

		now = time.Now()
		data = expr.Filter(data)
		timing.Add("query-exec", time.Since(now))
	}
	if !all {
		mut := data.Mutate()
		mut.Elide()
		data = mut.Data()
	}

	now = time.Now()
	buf, err := ottrecexph.HTML(data, &ottrecexph.Options{
		Raw:       raw,
		Indent:    " ",
		Canonical: "https://data.ottrec.ca/preview",
		Source:    "https://data.ottrec.ca/v1/" + id + "/pb",
		Script:    true,
		IncludeHead: func(x *xmlwriter.XMLWriter) error {
			x.Start(nil, "meta")
			x.Attr(nil, "name", "description")
			x.Attr(nil, "content", "Preview of all City of Ottawa drop-in recreation schedules on a single page.")
			x.End(true)
			x.Start(nil, "style")
			x.Raw([]byte("/*<![CDATA[*/" +
				".preview-nav{display:flex;justify-content:space-between;align-items:center;" +
				"padding:.5rem 1rem;background:light-dark(#fff,#111418);" +
				"border-bottom:1px solid light-dark(#dce2ee,#25303a);position:sticky;top:0;z-index:10}" +
				".preview-filter{padding:.3rem 1rem;background:light-dark(#f2f5fa,#1a1e2a);" +
				"border-bottom:1px solid light-dark(#dce2ee,#25303a);font-size:.8em;" +
				"color:light-dark(#5c6880,#7888a4)}" +
				".preview-filter code{font-family:monospace;color:light-dark(#18181c,#d8dce8)}" +
				"/*]]>*/",
			))
			x.End(false)
			return nil
		},
		IncludeTop: func(x *xmlwriter.XMLWriter) error {
			x.Start(nil, "nav")
			x.Attr(nil, "class", "preview-nav")
			x.Start(nil, "a")
			x.Attr(nil, "href", "https://data.ottrec.ca/")
			x.Text(false, "\u2190 data.ottrec.ca")
			x.End(false)
			x.Start(nil, "span")
			x.Start(nil, "a")
			x.Attr(nil, "href", "/export/latest.json")
			x.Attr(nil, "download", "ottrec_simplified_latest.json")
			x.Text(false, "JSON")
			x.End(false)
			x.Text(false, " ")
			x.Start(nil, "a")
			x.Attr(nil, "href", "/export/latest.csv.zip")
			x.Attr(nil, "download", "ottrec_simplified_latest.csv.zip")
			x.Text(false, "CSV")
			x.End(false)
			x.End(false)
			x.End(false)
			if q != "" {
				x.Start(nil, "div")
				x.Attr(nil, "class", "preview-filter")
				x.Text(false, "Filter: ")
				x.Start(nil, "code")
				x.Text(false, q)
				x.End(false)
				x.End(false)
			}
			return nil
		},
	})
	timing.Add("render", time.Since(now))
	if err != nil {
		slog.Error("preview: failed to render html", "id", id, "error", err)
		h.serveError(w, "internal server error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	d := w.Header()
	d.Set("Content-Length", strconv.Itoa(len(buf)))
	d.Set("Content-Type", "application/xhtml+xml; charset=utf-8")
	d.Set("ETag", etag)
	d.Set("Server-Timing", timing.String())
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		w.Write(buf)
	}
}

func (h *dataPreviewHandler) serveError(w http.ResponseWriter, message string, code int) {
	d := w.Header()
	d.Set("Content-Length", strconv.Itoa(len(message)+1))
	d.Set("Content-Type", "text/plain; charset=utf-8")
	d.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	io.WriteString(w, message+"\n")
}

type dataExportHandler struct {
	Base  string
	Cache *ottrecdata.Cache

	cacheMu sync.Mutex
	cache   map[string]weak.Pointer[dataExportData]

	latestMu sync.Mutex
	latest   *dataExportData
}

type dataExportData struct {
	id    string
	ready <-chan struct{}

	err      error
	csv      []byte
	csvETag  string
	csvErr   error
	json     []byte
	jsonETag string
	jsonErr  error
}

// lazy since not everything needs it, and to give a chance to set stuff like
// [ottrecsimple.JSONSchemaID]
var (
	dataExportSchemaJSON = sync.OnceValue(func() []byte {
		return append(ottrecexp.JSONSchema(), '\n')
	})
	dataExportSchemaCSV = sync.OnceValue(func() []byte {
		return ottrecexp.CSVSchema()
	})
)

func (h *dataExportHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		h.serveError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.URL.RawQuery != "" {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, r.URL.EscapedPath(), http.StatusTemporaryRedirect)
		return
	}

	if rest, ok := strings.CutPrefix(r.URL.Path, h.Base); ok {
		if rest == "schema.json" {
			h.serveSchemaJSON(w, r)
			return
		}
		if rest == "schema.csv" {
			h.serveSchemaCSV(w, r)
			return
		}
		if spec, ok := strings.CutSuffix(rest, ".json"); ok {
			h.serveJSON(w, r, spec)
			return
		}
		if spec, ok := strings.CutSuffix(rest, ".csv.zip"); ok {
			h.serveCSV(w, r, spec)
			return
		}
	}

	h.serveError(w, "not found", http.StatusNotFound)
}

func (h *dataExportHandler) redirectFile(w http.ResponseWriter, spec, ext string) {
	var u strings.Builder
	u.WriteString(h.Base)
	u.WriteString(spec)
	u.WriteString(url.PathEscape(ext))
	w.Header().Set("Location", u.String())
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusTemporaryRedirect)
}

func (h *dataExportHandler) serveError(w http.ResponseWriter, message string, code int) {
	d := w.Header()
	d.Set("Content-Length", strconv.Itoa(len(message)+1))
	d.Set("Content-Type", "text/plain; charset=utf-8")
	d.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	io.WriteString(w, message+"\n")
}

func (h *dataExportHandler) serveSchemaJSON(w http.ResponseWriter, r *http.Request) {
	b := dataExportSchemaJSON()
	d := w.Header()
	d.Set("Content-Length", strconv.Itoa(len(b)))
	d.Set("Content-Type", "application/schema+json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(b)
}

func (h *dataExportHandler) serveSchemaCSV(w http.ResponseWriter, r *http.Request) {
	b := dataExportSchemaCSV()
	d := w.Header()
	d.Set("Content-Length", strconv.Itoa(len(b)))
	d.Set("Content-Type", "text/csv; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(b)
}

func (h *dataExportHandler) serveCSV(w http.ResponseWriter, r *http.Request, spec string) {
	w.Header().Set("Cache-Control", "public, max-age=60")

	buf, etag, id, err := h.resolveCSV(r.Context(), spec)
	if err != nil {
		if errors.Is(err, errInvalidSpecFormat) {
			h.serveError(w, "invalid spec format "+strconv.Quote(spec), http.StatusBadRequest)
		} else {
			h.serveError(w, "internal error: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}
	if buf == nil {
		h.serveError(w, "no data found for "+strconv.Quote(spec), http.StatusNotFound)
		return
	}

	// if it isn't the canonical URL, redirect it to the canonical one (for
	// better caching) as long as it isn't a latest/latest-relative request (so
	// refreshing will still get the latest one for that).
	if !strings.HasPrefix(spec, "latest") && spec != id {
		h.redirectFile(w, id, ".csv.zip")
		return
	}

	w.Header().Set("Cache-Control", "public, no-cache")
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", "application/zip")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(buf))
}

func (h *dataExportHandler) serveJSON(w http.ResponseWriter, r *http.Request, spec string) {
	w.Header().Set("Cache-Control", "public, max-age=60")

	buf, etag, id, err := h.resolveJSON(r.Context(), spec)
	if err != nil {
		if errors.Is(err, errInvalidSpecFormat) {
			h.serveError(w, "invalid spec format "+strconv.Quote(spec), http.StatusBadRequest)
		} else {
			h.serveError(w, "internal error: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}
	if buf == nil {
		h.serveError(w, "no data found for "+strconv.Quote(spec), http.StatusNotFound)
		return
	}

	// if it isn't the canonical URL, redirect it to the canonical one (for
	// better caching) as long as it isn't a latest/latest-relative request (so
	// refreshing will still get the latest one for that).
	if !strings.HasPrefix(spec, "latest") && spec != id {
		h.redirectFile(w, id, ".json")
		return
	}

	w.Header().Set("Cache-Control", "public, no-cache")

	// TODO: negotiate and cache compression

	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", "application/json")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(buf))
}

var errInvalidSpecFormat = errors.New("invalid spec format")

func (h *dataExportHandler) resolve(spec string) (*dataExportData, error) {
	if spec == "" {
		spec = "latest"
	}

	if d := h.prepare(spec, true); d != nil {
		return d, nil
	}

	if spec == "latest" {
		// TODO: singleflight latest requests or cache for a short time?
		h.latestMu.Lock()
		defer h.latestMu.Unlock()
	}

	slog.Debug("export: resolving version", "spec", spec)
	id, _, ok, err := h.Cache.ResolveVersion(context.Background(), cmp.Or(spec, "latest"))
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", spec, err)
	}
	if !ok {
		return nil, errInvalidSpecFormat
	}
	if id == "" {
		return nil, nil
	}

	d := h.prepare(id, false)

	if spec == "latest" {
		var old string
		if h.latest != nil {
			old = h.latest.id
		}
		if old != id {
			slog.Info("export: got new latest version", "old", old, "new", id)
		}
		h.latest = d
	}

	return d, nil
}

func (h *dataExportHandler) prepare(id string, cachedOnly bool) *dataExportData {
	h.cacheMu.Lock()
	defer h.cacheMu.Unlock()

	if h.cache == nil {
		h.cache = make(map[string]weak.Pointer[dataExportData])
	}

	if d, ok := h.cache[id]; ok {
		if d := d.Value(); d != nil {
			slog.Debug("export: got cached export", "id", id)
			return d
		}
	}
	if cachedOnly {
		return nil
	}

	r := make(chan struct{})
	d := &dataExportData{
		id:    id,
		ready: r,
	}
	runtime.AddCleanup(d, func(id string) {
		slog.Info("export: freed unused cache", "id", id)
	}, id)
	h.cache[id] = weak.Make(d)

	var n int
	for _, p := range h.cache {
		if p.Value() != nil {
			n++
		}
	}
	slog.Info("export: preparing new cache entry", "id", id, "total", n)

	go func() {
		slog.Debug("export: preparing", "id", id)

		defer func() {
			if d.err != nil {
				slog.Error("export: failed", "id", id, "error", d.err)
			} else {
				if d.csvErr != nil {
					slog.Error("export: csv failed", "id", id, "error", d.csvErr)
				}
				if d.jsonErr != nil {
					slog.Error("export: json failed", "id", id, "error", d.jsonErr)
				}
				slog.Debug("export: done", "id", id, "csv_size", len(d.csv), "json_size", len(d.json))
			}
		}()

		d.err = func() error {
			defer close(r)

			var blob string
			var err error
			for hash, format := range h.Cache.DataFormats(context.Background(), id)(&err) {
				if format == "pb" {
					blob = hash
					break
				}
			}
			if err != nil {
				return fmt.Errorf("load data %q: resolve format: %w", id, err)
			}
			if blob == "" {
				return fmt.Errorf("load data %q: no pb found", id)
			}

			var pb []byte
			exists, err := h.Cache.ReadBlob(context.Background(), blob, false, func(r io.Reader, size int64) error {
				pb = make([]byte, size)
				_, err := io.ReadFull(r, pb)
				return err
			})
			if err != nil {
				return fmt.Errorf("load data %q: read pb: %w", id, err)
			}
			if !exists {
				return fmt.Errorf("load data %q: missing blob", id)
			}

			idx, err := new(ottrecidx.Indexer).Load(pb)
			if err != nil {
				return fmt.Errorf("load data %q: %w", id, err)
			}

			exp, err := ottrecexp.New(idx.Data())
			if err != nil {
				return fmt.Errorf("export data %q: %w", id, err)
			}

			buf := templ.GetBuffer()
			defer templ.ReleaseBuffer(buf)

			// note: we could have used the exehash and data hash as the etag to
			// be able to check it before actually doing the export, but export
			// is cheap, and this is simple enough (and still saves bandwidth,
			// which is the point)

			if err := exportCSV(buf, exp); err != nil {
				d.csvErr = err
			} else {
				sum := sha1.Sum(buf.Bytes())
				d.csv = slices.Clone(buf.Bytes())
				d.csvETag = `W/"` + base32.StdEncoding.EncodeToString(sum[:]) + `"`
			}
			d.csvErr = exportCSV(buf, exp)

			buf.Reset()

			if err := ottrecexp.WriteJSON(exp, buf); err != nil {
				d.jsonErr = err
			} else {
				sum := sha1.Sum(buf.Bytes())
				d.json = slices.Clone(buf.Bytes())
				d.jsonETag = `W/"` + base32.StdEncoding.EncodeToString(sum[:]) + `"`
			}
			buf.Reset()

			return nil
		}()
	}()

	return d
}

func (h *dataExportHandler) resolveCSV(ctx context.Context, spec string) ([]byte, string, string, error) {
	d, err := h.resolve(spec)
	if err != nil {
		return nil, "", "", err
	}
	if d == nil {
		return nil, "", "", nil
	}
	select {
	case <-ctx.Done():
		return nil, "", d.id, ctx.Err()
	case <-d.ready:
		if d.err != nil {
			return nil, "", d.id, err
		}
		return d.csv, d.csvETag, d.id, d.csvErr
	}
}

func (h *dataExportHandler) resolveJSON(ctx context.Context, spec string) ([]byte, string, string, error) {
	d, err := h.resolve(spec)
	if err != nil {
		return nil, "", "", err
	}
	if d == nil {
		return nil, "", "", nil
	}
	select {
	case <-ctx.Done():
		return nil, "", d.id, ctx.Err()
	case <-d.ready:
		if d.err != nil {
			return nil, "", d.id, err
		}
		return d.json, d.jsonETag, d.id, d.jsonErr
	}
}

func exportCSV(w io.Writer, exp *ottrecexp.Data) error {
	zw := zip.NewWriter(w)
	{
		w, err := zw.Create("schema.csv")
		if err != nil {
			return err
		}
		w.Write(dataExportSchemaCSV())
	}
	var serr error
	if err := ottrecexp.WriteCSV(exp, func(table string) io.Writer {
		if serr != nil {
			return nil
		}
		w, err := zw.Create(table + ".csv")
		if err != nil {
			serr = err
			return nil
		}
		return w
	}); err != nil {
		return err
	}
	if serr != nil {
		return serr
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return nil
}

type dataAPIv1 struct {
	Base  string
	Cache *ottrecdata.Cache
}

func (h *dataAPIv1) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex")

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		h.serveError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")

	if rest, ok := strings.CutPrefix(r.URL.Path, h.Base); ok {
		if rest == "" {
			h.serveList(w, r)
			return
		}
		if spec, format, _ := strings.Cut(rest, "/"); !strings.Contains(format, "/") {
			h.serveFile(w, r, spec, format)
			return
		}
	}

	h.serveError(w, "not found", http.StatusNotFound)
}

func (h *dataAPIv1) serveError(w http.ResponseWriter, message string, code int) {
	d := w.Header()
	d.Set("Content-Length", strconv.Itoa(len(message)+1))
	d.Set("Content-Type", "text/plain; charset=utf-8")
	d.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	io.WriteString(w, message+"\n")
}

func (h *dataAPIv1) serveList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// validate query
	var (
		after           = ""
		limit, maxLimit = 25, 500
		revisions       = false
	)
	for k, v := range r.URL.Query() {
		if len(v) == 0 {
			continue
		}
		switch k {
		case "limit":
			v, err := strconv.ParseInt(v[0], 10, 64)
			if err != nil {
				h.serveError(w, "invalid limit int", http.StatusBadRequest)
				return
			}
			limit = int(v)
		case "after":
			after = v[0]
		case "revisions":
			v, err := strconv.ParseBool(v[0])
			if err != nil {
				h.serveError(w, "invalid revisions bool", http.StatusBadRequest)
				return
			}
			revisions = v
		default:
			h.serveError(w, "invalid parameter "+strconv.Quote(k), http.StatusBadRequest)
			return
		}
	}
	if limit <= 0 || limit > maxLimit {
		h.serveError(w, "limit out of range", http.StatusBadRequest)
		return
	}
	if after != "" && !ottrecdata.IsID(after) {
		h.serveError(w, "after is not a valid data id", http.StatusBadRequest)
		return
	}

	// cache the list for a minute
	w.Header().Set("Cache-Control", "public, max-age=60")

	// set the mimetype
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	// no body for head requests
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	// generate the json
	var (
		err       error
		wrote     bool
		bw        = bufio.NewWriterSize(w, 512)
		seenAfter bool
	)
	for prev, ver := range iterPrev(h.Cache.DataVersions(ctx)(&err)) {
		if after != "" && !seenAfter {
			if ver.ID == after {
				seenAfter = true
			}
			continue
		}
		if !revisions && prev.Updated.Equal(ver.Updated) {
			continue // this must be after the after check, or we might miss revisions
		}
		if limit--; limit < 0 {
			break
		}
		if !wrote {
			wrote = true
			bw.WriteByte('[')
		} else {
			bw.WriteByte(',')
		}
		bw.WriteString(`{"id":"`)
		bw.WriteString(ver.ID)
		bw.WriteString(`","updated":"`)
		bw.WriteString(ver.Updated.In(ottrecdata.TZ).Format(time.RFC3339))
		bw.WriteString(`","revision":`)
		bw.Write(strconv.AppendInt(bw.AvailableBuffer(), int64(ver.Revision), 10))
		bw.WriteString(`}`)
	}
	if !wrote {
		bw.WriteByte('[')
	}
	bw.WriteString("]\n")
	bw.Flush()
	if err != nil {
		if canceled := ctx.Err() != nil; !canceled {
			slog.Error("data api v1: failed to serve list", "error", err)
			if wrote {
				io.WriteString(w, "\ninternal server error: "+err.Error()+"\n")
			} else {
				h.serveError(w, "internal server error: "+err.Error(), http.StatusInternalServerError)
			}
		}
		return
	}
}

func (h *dataAPIv1) serveFile(w http.ResponseWriter, r *http.Request, spec, format string) {
	ctx := r.Context()

	// we do content encoding negotiation
	w.Header().Add("Vary", "Accept-Encoding")

	// validate query
	for k := range r.URL.Query() {
		h.serveError(w, "invalid parameter "+strconv.Quote(k), http.StatusBadRequest)
		return
	}

	// resolve the data version spec
	id, updated, ok, err := h.Cache.ResolveVersion(ctx, cmp.Or(spec, "latest"))
	if err != nil {
		slog.Error("data api v1: failed to resolve spec", "spec", spec, "error", err)
		h.serveError(w, "internal server error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		h.serveError(w, "invalid spec format "+strconv.Quote(spec), http.StatusBadRequest)
		return
	}

	// put data update time in a header if known
	if !updated.IsZero() {
		// note: note Last-Modified since it isn't technically correct for this
		w.Header().Set("X-Schedule-Updated", updated.UTC().Format(http.TimeFormat))
	}

	// cache data resolution for 60s
	w.Header().Set("Cache-Control", "public, max-age=60")

	// no data matching spec
	if id == "" {
		if spec == "" || spec == "latest" {
			slog.Error("data api v1: no data available")
			h.serveError(w, "no data available, try again later", http.StatusServiceUnavailable)
		} else {
			h.serveError(w, "no match for "+strconv.Quote(spec), http.StatusNotFound)
		}
		return
	}

	// redirect to canonical url for data id
	if spec != id {
		h.redirectFile(w, id, format)
		return
	}

	// redirect to pb if no format specified
	if format == "" {
		h.redirectFile(w, id, string("pb"))
		return
	}

	// validate the format and set mimetype
	switch format {
	case "pb":
		w.Header().Set("Content-Type", "application/x-protobuf")
	case "proto":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	case "json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	case "textpb":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	default:
		h.serveError(w, "unknown format", http.StatusNotFound)
		return
	}

	// select the format and set mimetype
	var hash string
	for h, f := range h.Cache.DataFormats(ctx, id)(&err) {
		if f == format {
			hash = h
			break
		}
	}
	if err != nil {
		slog.Error("data api v1: failed to resolve formats", "id", id, "error", err)
		h.serveError(w, "internal server error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if hash == "" {
		h.serveError(w, "format not found", http.StatusNotFound)
		return
	}

	// negotiate encoding
	encoding := httpx.NegotiateContent(r.Header.Values("Accept-Encoding"), []string{"", "gzip"})
	if encoding != "" {
		w.Header().Set("Content-Encoding", encoding)
	}

	// cache the data for longer since it's immutable (but don't say immutable
	// just in case we have bugs somewhere)
	w.Header().Set("Cache-Control", "public, max-age=604800")

	// build etag from content hash and encoding
	var etag strings.Builder
	etag.WriteString(`W/"`)
	etag.WriteString(hash)
	if encoding != "" {
		etag.WriteByte('-')
		etag.WriteString(encoding)
	}
	etag.WriteString(`"`)
	w.Header().Set("ETag", etag.String())

	// check etag match
	if slices.Contains(r.Header.Values("If-None-Match"), etag.String()) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	// no body for head requests
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	// serve the file
	ok, err = h.Cache.ReadBlob(ctx, hash, encoding == "gzip", func(r io.Reader, len int64) error {
		if len != -1 {
			w.Header().Set("Content-Length", strconv.FormatInt(len, 10))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, r)
		return nil
	})
	if err != nil {
		if canceled := r.Context().Err() != nil; !canceled {
			slog.Error("data api v1: failed to serve blob", "hash", hash, "encoding", encoding, "error", err)
			h.serveError(w, "internal server error: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}
	if !ok {
		slog.Error("data api v1: missing blob", "hash", hash, "encoding", encoding)
		h.serveError(w, "internal server error: missing blob", http.StatusInternalServerError)
		return
	}
}

func (h *dataAPIv1) redirectFile(w http.ResponseWriter, spec, format string) {
	var u strings.Builder
	u.WriteString(h.Base)
	u.WriteString(spec)
	if format != "" {
		u.WriteString("/")
		u.WriteString(url.PathEscape(format))
	}
	w.Header().Set("Location", u.String())
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusTemporaryRedirect)
}
