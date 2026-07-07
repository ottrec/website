package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/ottrec/data-enrichment/enrichidx"
	"github.com/ottrec/data-enrichment/report"
	"github.com/ottrec/website/internal/httpx"
	"github.com/ottrec/website/pkg/ottrecidx"
	"github.com/ottrec/website/pkg/ottrecql"
	"github.com/ottrec/website/static"
	"github.com/ottrec/website/templates"
)

type WebsiteConfig struct {
	Data func() (ottrecidx.DataRef, bool)
	// Enrich returns the schedule-change enrichment derived from the given
	// data, or a zero Ref if none is available (enrichment is a progressive
	// enhancement; nil is fine).
	Enrich func(ottrecidx.DataRef) enrichidx.Ref
	// HeadHTML is raw HTML injected at the bottom of <head> on every page.
	HeadHTML string
	// AboutHTML is raw HTML injected at the bottom of the /about page.
	AboutHTML string
	// HomeHTML is raw HTML injected at the bottom of the homepage's <main>.
	HomeHTML string
}

func Website(cfg WebsiteConfig) (http.Handler, error) {
	if cfg.Data == nil {
		return nil, fmt.Errorf("no data getter specified")
	}
	templates.SetHeadExtra(cfg.HeadHTML)
	templates.SetAboutExtra(cfg.AboutHTML)
	templates.SetHomeExtra(cfg.HomeHTML)

	base := websiteHandlerBase{
		Data:   cfg.Data,
		Enrich: cfg.Enrich,
	}
	mux := http.NewServeMux()

	// TODO: fonts
	// TODO: base url for rel=canonical

	mux.Handle("GET /{$}", &websiteHomeHandler{
		websiteHandlerBase: base,
	})
	mux.Handle("GET /map", &websiteMapHandler{
		websiteHandlerBase: base,
	})
	mux.Handle("GET /map/facility/{slug}", &websiteMapFacilityHandler{
		websiteHandlerBase: base,
	})
	mux.Handle("GET /activities", &websiteActivitiesHandler{
		websiteHandlerBase: base,
	})
	mux.Handle("GET /today", &websiteTodayHandler{
		websiteHandlerBase: base,
	})
	mux.Handle("GET /api/changes", &websiteTodayChangesHandler{
		websiteHandlerBase: base,
	})
	mux.Handle("GET /api/errors", &websiteTodayErrorsHandler{
		websiteHandlerBase: base,
	})
	mux.Handle("GET /api/reservations", &websiteTodayReservationsHandler{
		websiteHandlerBase: base,
	})
	mux.Handle("GET /api/enrich/report", &websiteEnrichReportHandler{
		websiteHandlerBase: base,
	})
	mux.Handle("GET /schedules", &websiteSchedulesHandler{
		websiteHandlerBase: base,
	})
	mux.Handle("GET /schedules/{key}", &websiteSchedulesKeyHandler{
		websiteHandlerBase: base,
	})
	mux.Handle("GET /schedules/facility/{slug}", &websiteSchedulesFacilityHandler{
		websiteHandlerBase: base,
	})
	mux.Handle("GET /robots.txt", &websiteRobotsHandler{
		websiteHandlerBase: base,
	})
	mux.Handle("GET /sitemap.xml", &websiteSitemapHandler{
		websiteHandlerBase: base,
	})
	mux.Handle("GET /api/ottrecql/validate", &websiteOttrecqlValidateHandler{})
	mux.Handle("GET /api/ottrecql/facilities", &websiteOttrecqlNamesHandler{
		websiteHandlerBase: base,
	})
	mux.Handle("GET /api/ottrecql/activities", &websiteOttrecqlNamesHandler{
		websiteHandlerBase: base,
		activities:         true,
	})
	mux.Handle("GET /about", &websiteAboutHandler{
		websiteHandlerBase: base,
	})
	mux.Handle("GET /about/ottrecql", &websiteOttrecqlHandler{
		websiteHandlerBase: base,
	})
	for _, slug := range aboutContentPages {
		mux.Handle("GET /about/"+slug, &websiteAboutContentHandler{
			websiteHandlerBase: base,
			slug:               slug,
		})
	}
	mux.Handle("GET /api/regions/layer.png", &websiteRegionsLayerHandler{})
	mux.Handle("/static/", static.Handler(static.Website))
	mux.Handle("GET /favicon.ico", static.Handler(static.Website))

	// catch-all for unmatched paths (more specific patterns above win); without
	// it, http.ServeMux returns a plain-text 404. No method, so it doesn't
	// conflict with the method-less /static/ subtree.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		templates.RenderError(w, r, templates.WebsiteErrorPage, "Not Found", "We couldn't find the page you're looking for.", http.StatusNotFound)
	})

	return commonMiddleware(redirectTrailingSlash(mux)), nil
}

// redirectTrailingSlash permanently redirects paths with trailing slashes to
// the canonical slash-less ones. The static subtree is excluded since
// [http.ServeMux] redirects the other way for subtree roots.
func redirectTrailingSlash(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := r.URL.Path; len(p) > 1 && strings.HasSuffix(p, "/") && !strings.HasPrefix(p, "/static/") {
			u := *r.URL
			u.Path = strings.TrimRight(p, "/")
			if u.Path == "" {
				u.Path = "/"
			}
			http.Redirect(w, r, u.String(), http.StatusPermanentRedirect)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type websiteHandlerBase struct {
	Host   string
	Data   func() (ottrecidx.DataRef, bool)
	Enrich func(ottrecidx.DataRef) enrichidx.Ref
}

// enrich returns the enrichment for the given data, or a zero Ref when
// unavailable.
func (h *websiteHandlerBase) enrich(data ottrecidx.DataRef) enrichidx.Ref {
	if h.Enrich == nil {
		return enrichidx.Ref{}
	}
	return h.Enrich(data)
}

func (h *websiteHandlerBase) render(w http.ResponseWriter, r *http.Request, fn func(data ottrecidx.DataRef) (c templ.Component, status int, err error)) {
	h.renderETag(w, r, "", fn)
}

// renderETag is render with an extra etag mixin for pages whose output depends
// on more than the dataset (e.g. the current date).
func (h *websiteHandlerBase) renderETag(w http.ResponseWriter, r *http.Request, etagMixin string, fn func(data ottrecidx.DataRef) (c templ.Component, status int, err error)) {
	var (
		data ottrecidx.DataRef
		ok   bool
	)
	if h.Data != nil {
		data, ok = h.Data()
	}
	if !ok {
		slog.Error("website: no data available")
		templates.RenderError(w, r, templates.WebsiteErrorPage, "Data Unavailable", "data not available, try again later", http.StatusServiceUnavailable)
		return
	}
	if err := templates.Render(w, r, templates.WebsiteErrorPage, data.Index().Hash()+"\x00"+etagMixin, func() (c templ.Component, status int, err error) {
		return fn(data)
	}); err != nil {
		slog.Error("website: failed to render page", "url", r.URL.String(), "error", err)
	}
}

func (h *websiteHandlerBase) base(r *http.Request) string {
	return "https://ottrec.ca/"
}

type websiteHomeHandler struct {
	websiteHandlerBase
}

func (h *websiteHomeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Accept-Encoding")
	w.Header().Set("Cache-Control", "public, no-cache")

	if r.URL.RawQuery != "" {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, r.URL.EscapedPath(), http.StatusTemporaryRedirect)
		return
	}

	// TODO: cache headers

	h.render(w, r, func(data ottrecidx.DataRef) (templ.Component, int, error) {
		return templates.WebsiteHomePage(templates.WebsiteParams{
			Base: h.base(r),
			Data: data,
		}), http.StatusOK, nil
	})
}

type websiteMapHandler struct {
	websiteHandlerBase
}

func (h *websiteMapHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Accept-Encoding")
	w.Header().Set("Cache-Control", "public, no-cache")

	// the client-side f-* filter params don't affect the page output, so keep
	// them, but canonicalize anything else away
	if r.URL.RawQuery != "" {
		q := r.URL.Query()
		clean := true
		for k := range q {
			if !strings.HasPrefix(k, "f-") {
				delete(q, k)
				clean = false
			}
		}
		if !clean {
			w.Header().Set("Cache-Control", "no-store")
			u := r.URL.EscapedPath()
			if enc := q.Encode(); enc != "" {
				u += "?" + enc
			}
			http.Redirect(w, r, u, http.StatusTemporaryRedirect)
			return
		}
	}

	h.render(w, r, func(data ottrecidx.DataRef) (templ.Component, int, error) {
		return templates.WebsiteMapPage(templates.WebsiteParams{
			Base: h.base(r),
			Data: data,
		}), http.StatusOK, nil
	})
}

// websiteSchedulesHandler serves the root schedules page, with an optional
// server-side search (?q=...; simple activity/facility name matching by
// default, or a full ottrecql query with ?advanced=1).
type websiteSchedulesHandler struct {
	websiteHandlerBase
}

func (h *websiteSchedulesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Accept-Encoding")
	w.Header().Set("Cache-Control", "public, no-cache")

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	advanced := r.URL.Query().Get("advanced") != ""
	list := r.URL.Query().Get("mode") == "list"
	if r.URL.RawQuery != "" && q == "" && !advanced && !list {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, r.URL.EscapedPath(), http.StatusTemporaryRedirect)
		return
	}

	h.render(w, r, func(data ottrecidx.DataRef) (templ.Component, int, error) {
		params := templates.WebsiteSchedulesParams{
			Base:            h.base(r),
			Data:            data,
			Canonical:       "schedules",
			Path:            "/schedules",
			Active:          "all",
			Title:           "Schedules",
			Description:     "Drop-in schedules across all City of Ottawa recreation facilities.",
			MetaDescription: "All City of Ottawa drop-in recreation schedules on one page, searchable and updated daily.",
			Search:          true,
			Advanced:        advanced,
			Query:           q,
			List:            list,
		}
		filtered := data
		switch {
		case q == "":
			filtered = templates.SchedulesElide(data)
		case advanced:
			node, err := templates.SchedulesParseQuery(q)
			if err == nil {
				filtered, err = templates.SchedulesFilter(data, node)
			}
			if err != nil {
				params.QueryError = err.Error()
			}
		default:
			if len(q) > templates.SchedulesMaxQueryLen {
				params.QueryError = fmt.Sprintf("query too long (max %d bytes)", templates.SchedulesMaxQueryLen)
				break
			}
			var err error
			filtered, err = templates.SchedulesFilter(data, templates.SchedulesSearchQuery(q))
			if err != nil {
				return nil, 0, err
			}
		}
		if params.QueryError == "" {
			params.TOC = templates.SchedulesTOC(filtered, templates.MapFacilitySlugger(data))
		}
		return templates.WebsiteSchedulesPage(params), http.StatusOK, nil
	})
}

// aboutContentPages is the hard-coded list of generic /about/{slug} pages, each
// backed by a templates/about-{slug}.md markdown file. It drives both handler
// registration and the sitemap, so it isn't derived from the embedded directory.
var aboutContentPages = []string{
	"data-quality",
	"regions",
}

type websiteAboutHandler struct {
	websiteHandlerBase
}

func (h *websiteAboutHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Accept-Encoding")
	w.Header().Set("Cache-Control", "public, no-cache")

	if r.URL.RawQuery != "" {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, r.URL.EscapedPath(), http.StatusTemporaryRedirect)
		return
	}

	h.render(w, r, func(data ottrecidx.DataRef) (templ.Component, int, error) {
		return templates.WebsiteAboutPage(templates.WebsiteParams{
			Base: h.base(r),
			Data: data,
		}), http.StatusOK, nil
	})
}

// websiteAboutContentHandler serves a generic /about/{slug} page rendered from
// the templates/about-{slug}.md markdown file.
type websiteAboutContentHandler struct {
	websiteHandlerBase
	slug string
}

func (h *websiteAboutContentHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Accept-Encoding")
	w.Header().Set("Cache-Control", "public, no-cache")

	if r.URL.RawQuery != "" {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, r.URL.EscapedPath(), http.StatusTemporaryRedirect)
		return
	}

	content, ok := templates.AboutContentBySlug(h.slug)
	if !ok {
		// shouldn't happen: handlers are registered from the same list, and the
		// markdown is rendered at startup, but guard anyway.
		templates.RenderError(w, r, templates.WebsiteErrorPage, "Not Found", "We couldn't find the page you're looking for.", http.StatusNotFound)
		return
	}

	h.render(w, r, func(data ottrecidx.DataRef) (templ.Component, int, error) {
		return templates.WebsiteAboutContentPage(templates.WebsiteAboutContentParams{
			Base:    h.base(r),
			Data:    data,
			Content: content,
		}), http.StatusOK, nil
	})
}

// websiteOttrecqlHandler serves the standalone /about/ottrecql page documenting
// the query language, with a prominent search box submitting to /schedules.
type websiteOttrecqlHandler struct {
	websiteHandlerBase
}

func (h *websiteOttrecqlHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Accept-Encoding")
	w.Header().Set("Cache-Control", "public, no-cache")

	if r.URL.RawQuery != "" {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, r.URL.EscapedPath(), http.StatusTemporaryRedirect)
		return
	}

	h.render(w, r, func(data ottrecidx.DataRef) (templ.Component, int, error) {
		return templates.WebsiteOttrecqlPage(templates.WebsiteParams{
			Base: h.base(r),
			Data: data,
		}), http.StatusOK, nil
	})
}

// websiteOttrecqlValidateHandler validates an ottrecql query (with the same
// limits as the schedules advanced search), for live validation as the user
// types.
type websiteOttrecqlValidateHandler struct{}

func (h *websiteOttrecqlValidateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var resp struct {
		Error  string `json:"error,omitempty"`
		Offset *int   `json:"offset,omitempty"`
	}
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		if _, err := templates.SchedulesParseQuery(q); err != nil {
			resp.Error = err.Error()
			var perr *ottrecql.ParseError
			if errors.As(err, &perr) {
				resp.Offset = &perr.Offset
			}
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(resp)
}

// websiteOttrecqlNamesHandler serves the distinct facility or activity names as
// a JSON array, fetched once by the editor to autocomplete facility()/activity()
// string arguments. Cached by data hash like the sitemap.
type websiteOttrecqlNamesHandler struct {
	websiteHandlerBase
	activities bool
}

func (h *websiteOttrecqlNamesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	data, ok := h.Data()
	if !ok {
		http.Error(w, "data not available, try again later", http.StatusServiceUnavailable)
		return
	}

	seen := map[string]struct{}{}
	names := []string{}
	add := func(n string) {
		if n == "" {
			return
		}
		if _, dup := seen[n]; dup {
			return
		}
		seen[n] = struct{}{}
		names = append(names, n)
	}
	if h.activities {
		for a := range data.Activities() {
			add(a.GetName())
		}
	} else {
		for f := range data.Facilities() {
			add(f.GetName())
		}
	}
	slices.Sort(names)

	buf, err := json.Marshal(names)
	if err != nil {
		slog.Error("website: failed to marshal ottrecql names", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	etag := httpx.NewETag().
		MixExe().
		Mix(data.Index().Hash()).
		ETag().
		Weaken() // weak: built from the data hash, not the response bytes
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, no-cache")
	if etag.Handled(w, r) {
		return
	}
	w.Write(buf)
}

// websiteSchedulesKeyHandler serves the category schedule pages under
// /schedules/, redirecting old facility page paths to /schedules/facility/.
type websiteSchedulesKeyHandler struct {
	websiteHandlerBase
}

func (h *websiteSchedulesKeyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Accept-Encoding")
	w.Header().Set("Cache-Control", "public, no-cache")

	list := r.URL.Query().Get("mode") == "list"
	if r.URL.RawQuery != "" && !list {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, r.URL.EscapedPath(), http.StatusTemporaryRedirect)
		return
	}

	key := r.PathValue("key")
	if _, ok := templates.ScheduleCategoryBySlug(key); !ok {
		// the facility pages used to live at /schedules/{slug}
		if data, ok := h.Data(); ok {
			if _, ok := templates.MapFacilityBySlug(data, key); ok {
				target := "/schedules/facility/" + url.PathEscape(key)
				if list {
					target += "?mode=list"
				}
				w.Header().Set("Cache-Control", "no-store")
				http.Redirect(w, r, target, http.StatusPermanentRedirect)
				return
			}
		}
	}
	h.render(w, r, func(data ottrecidx.DataRef) (templ.Component, int, error) {
		cat, ok := templates.ScheduleCategoryBySlug(key)
		if !ok {
			return templates.WebsiteErrorPage("Not Found", "no schedule category matching "+strconv.Quote(key)), http.StatusNotFound, nil
		}
		filtered, err := templates.SchedulesFilter(data, cat.Query())
		if err != nil {
			return nil, 0, err
		}
		return templates.WebsiteSchedulesPage(templates.WebsiteSchedulesParams{
			Base:            h.base(r),
			Data:            data,
			Canonical:       "schedules/" + cat.Slug,
			Path:            "/schedules/" + cat.Slug,
			Active:          cat.Slug,
			Title:           cat.Name + " Schedules",
			Description:     cat.Description,
			MetaDescription: "City of Ottawa drop-in " + strings.ToLower(cat.Name) + " schedules across all recreation facilities.",
			List:            list,
			CategoryTerms:   cat.Activities,
			TOC:             templates.SchedulesTOC(filtered, templates.MapFacilitySlugger(data)),
		}), http.StatusOK, nil
	})
}

// websiteSchedulesFacilityHandler serves the single-facility schedule pages.
type websiteSchedulesFacilityHandler struct {
	websiteHandlerBase
}

func (h *websiteSchedulesFacilityHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Accept-Encoding")
	w.Header().Set("Cache-Control", "public, no-cache")

	list := r.URL.Query().Get("mode") == "list"
	if r.URL.RawQuery != "" && !list {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, r.URL.EscapedPath(), http.StatusTemporaryRedirect)
		return
	}

	slug := r.PathValue("slug")
	h.render(w, r, func(data ottrecidx.DataRef) (templ.Component, int, error) {
		fac, ok := templates.MapFacilityBySlug(data, slug)
		if !ok {
			return templates.WebsiteErrorPage("Not Found", "no facility matching "+strconv.Quote(slug)), http.StatusNotFound, nil
		}
		return templates.WebsiteSchedulesPage(templates.WebsiteSchedulesParams{
			Base:        h.base(r),
			Data:        data,
			Canonical:   "schedules",
			Path:        "/schedules/facility/" + url.PathEscape(slug),
			Title:       fac.GetName(),
			Description: "Drop-in recreation schedules for " + fac.GetName() + " in Ottawa.",
			Single:      true,
			List:        list,
			TOC:         templates.SchedulesFacilityTOC(slug, fac),
		}), http.StatusOK, nil
	})
}

// websiteRobotsHandler serves robots.txt, disallowing the API and the
// HTML/page fragments which canonicalize elsewhere.
type websiteRobotsHandler struct {
	websiteHandlerBase
}

func (h *websiteRobotsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body := "User-agent: *\n" +
		"Disallow: /api/\n" +
		"Disallow: /map/facility/\n" +
		"Disallow: /schedules/facility/\n" +
		"\n" +
		"Sitemap: " + h.base(r) + "sitemap.xml\n"
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, no-cache")
	w.Write([]byte(body))
}

// websiteSitemapHandler generates the sitemap from the indexable pages, with
// the data timestamp as the last modification date.
type websiteSitemapHandler struct {
	websiteHandlerBase
}

func (h *websiteSitemapHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	data, ok := h.Data()
	if !ok {
		http.Error(w, "data not available, try again later", http.StatusServiceUnavailable)
		return
	}

	paths := []string{"", "today", "map", "schedules", "activities", "about", "about/ottrecql"}
	for _, slug := range aboutContentPages {
		paths = append(paths, "about/"+slug)
	}
	for _, cat := range templates.ScheduleCategories {
		paths = append(paths, "schedules/"+cat.Slug)
	}

	urls := make([]string, len(paths))
	for i, p := range paths {
		urls[i] = h.base(r) + p
	}

	buf, err := sitemapXML(data.Index().Updated().In(ottrecidx.TZ).Format("2006-01-02"), urls)
	if err != nil {
		slog.Error("website: failed to render sitemap", "error", err)
		http.Error(w, "internal server error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	etag := httpx.NewETag().
		MixExe().
		Mix(data.Index().Hash()).
		ETag().
		Weaken() // weak: built from the data hash, not the response bytes
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, no-cache")
	if etag.Handled(w, r) {
		return
	}
	w.Write(buf)
}

type websiteActivitiesHandler struct {
	websiteHandlerBase
}

func (h *websiteActivitiesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Accept-Encoding")
	w.Header().Set("Cache-Control", "public, no-cache")

	if r.URL.RawQuery != "" {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, r.URL.EscapedPath(), http.StatusTemporaryRedirect)
		return
	}

	h.render(w, r, func(data ottrecidx.DataRef) (templ.Component, int, error) {
		return templates.WebsiteActivitiesPage(templates.WebsiteParams{
			Base: h.base(r),
			Data: data,
		}), http.StatusOK, nil
	})
}

// websiteTodayHandler serves the chronological "what's on" feed. Like the map,
// it keeps the client-side f-* filter params (which don't affect the rendered
// output) for shareable links, but canonicalizes anything else away.
type websiteTodayHandler struct {
	websiteHandlerBase
}

func (h *websiteTodayHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Accept-Encoding")
	w.Header().Set("Cache-Control", "public, no-cache")

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	advanced := r.URL.Query().Get("advanced") != ""
	noEnrich, _ := strconv.ParseBool(r.URL.Query().Get("noenrich")) // hidden switch to disable the change enrichment

	// keep the advanced search params and the client-side f-* filter params (the
	// latter don't affect the rendered output) for shareable links, but
	// canonicalize anything else away.
	if r.URL.RawQuery != "" {
		qq := r.URL.Query()
		clean := true
		for k := range qq {
			// advanced search params, plus the client-side f-* filter params
			// (which don't affect the rendered output); q only matters in
			// advanced mode, where the feed is filtered server-side. a truthy
			// noenrich stays since it changes the rendered output (and thus
			// must stay in the url for the etag); other values are the default.
			if k == "advanced" || (k == "q" && advanced) || (k == "noenrich" && noEnrich) || strings.HasPrefix(k, "f-") {
				continue
			}
			delete(qq, k)
			clean = false
		}
		if !clean {
			w.Header().Set("Cache-Control", "no-store")
			u := r.URL.EscapedPath()
			if enc := qq.Encode(); enc != "" {
				u += "?" + enc
			}
			http.Redirect(w, r, u, http.StatusTemporaryRedirect)
			return
		}
	}

	// the feed is anchored to the current date, so the etag must change with it
	h.renderETag(w, r, templates.TodayFeedDate(), func(data ottrecidx.DataRef) (templ.Component, int, error) {
		params := templates.WebsiteTodayParams{
			Base:     h.base(r),
			Data:     data,
			Filtered: data,
			Advanced: advanced,
			Query:    q,
		}
		if !noEnrich {
			params.Enrich = h.enrich(data)
		}
		if advanced && q != "" {
			node, err := templates.SchedulesParseQuery(q)
			if err == nil {
				params.Filtered, err = templates.SchedulesFilter(data, node)
			}
			if err != nil {
				params.QueryError = err.Error()
			}
		}
		return templates.WebsiteTodayPage(params), http.StatusOK, nil
	})
}

// websiteTodayChangesHandler serves the HTML fragment for the today page's
// schedule-changes modal: the schedule changes for a facility's group plus its
// notifications and special hours.
type websiteTodayChangesHandler struct {
	websiteHandlerBase
}

func (h *websiteTodayChangesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Accept-Encoding")
	w.Header().Set("Cache-Control", "public, no-cache")

	slug := r.URL.Query().Get("facility")
	group, _ := strconv.Atoi(r.URL.Query().Get("group"))
	h.render(w, r, func(data ottrecidx.DataRef) (templ.Component, int, error) {
		fac, ok := templates.MapFacilityBySlug(data, slug)
		if !ok {
			return templates.WebsiteErrorPage("Not Found", "no facility matching "+strconv.Quote(slug)), http.StatusNotFound, nil
		}
		grp, hasGroup := templates.FacilityGroupAt(fac, group)
		return templates.WebsiteTodayChanges(templates.WebsiteTodayChangesParams{
			Facility: fac,
			Group:    grp,
			HasGroup: hasGroup,
		}), http.StatusOK, nil
	})
}

// websiteTodayErrorsHandler serves the HTML fragment for the today page's
// "possibly incomplete" modal: a facility's scrape errors.
type websiteTodayErrorsHandler struct {
	websiteHandlerBase
}

func (h *websiteTodayErrorsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Accept-Encoding")
	w.Header().Set("Cache-Control", "public, no-cache")

	slug := r.URL.Query().Get("facility")
	h.render(w, r, func(data ottrecidx.DataRef) (templ.Component, int, error) {
		fac, ok := templates.MapFacilityBySlug(data, slug)
		if !ok {
			return templates.WebsiteErrorPage("Not Found", "no facility matching "+strconv.Quote(slug)), http.StatusNotFound, nil
		}
		return templates.WebsiteTodayErrors(fac), http.StatusOK, nil
	})
}

// websiteTodayReservationsHandler serves the HTML fragment for the today page's
// "reservation required" modal: the facility/group name and the group's
// reservation links.
type websiteTodayReservationsHandler struct {
	websiteHandlerBase
}

func (h *websiteTodayReservationsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Accept-Encoding")
	w.Header().Set("Cache-Control", "public, no-cache")

	slug := r.URL.Query().Get("facility")
	group, _ := strconv.Atoi(r.URL.Query().Get("group"))
	h.render(w, r, func(data ottrecidx.DataRef) (templ.Component, int, error) {
		fac, ok := templates.MapFacilityBySlug(data, slug)
		if !ok {
			return templates.WebsiteErrorPage("Not Found", "no facility matching "+strconv.Quote(slug)), http.StatusNotFound, nil
		}
		grp, hasGroup := templates.FacilityGroupAt(fac, group)
		return templates.WebsiteTodayReservations(templates.WebsiteTodayReservationsParams{
			Facility: fac,
			Group:    grp,
			HasGroup: hasGroup,
		}), http.StatusOK, nil
	})
}

// websiteEnrichReportHandler serves the enrichment debugging report from
// data-enrichment/report for the current dataset. Unlinked and noindex, for
// internal use; it reruns the enrichment on a cache miss, so it's not cheap.
type websiteEnrichReportHandler struct {
	websiteHandlerBase
}

func (h *websiteEnrichReportHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex")

	var (
		data ottrecidx.DataRef
		ok   bool
	)
	if h.Data != nil {
		data, ok = h.Data()
	}
	if !ok {
		http.Error(w, "data not available, try again later", http.StatusServiceUnavailable)
		return
	}

	etag := httpx.NewETag().
		MixExe().
		Mix(data.Index().Hash()).
		ETag().
		Weaken() // weak: built from the data hash, not the response bytes
	w.Header().Set("Cache-Control", "public, no-cache")
	if etag.Handled(w, r) {
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(report.Build("", data))
}

// websiteMapFacilityHandler serves the HTML fragment fetched over XHR for the
// map facility popups.
type websiteMapFacilityHandler struct {
	websiteHandlerBase
}

func (h *websiteMapFacilityHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Accept-Encoding")
	w.Header().Set("Cache-Control", "public, no-cache")

	// the only allowed query param is `group` (a schedule group index, used by
	// the /today full-schedule modal); canonicalize anything else away so the
	// URLs stay cacheable.
	var group *int
	if g := r.URL.Query().Get("group"); g != "" {
		if n, err := strconv.Atoi(g); err == nil && n >= 0 {
			group = &n
		}
	}
	canonicalQuery := ""
	if group != nil {
		canonicalQuery = "group=" + strconv.Itoa(*group)
	}
	if r.URL.RawQuery != canonicalQuery {
		w.Header().Set("Cache-Control", "no-store")
		target := r.URL.EscapedPath()
		if canonicalQuery != "" {
			target += "?" + canonicalQuery
		}
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
		return
	}

	slug := r.PathValue("slug")
	h.render(w, r, func(data ottrecidx.DataRef) (templ.Component, int, error) {
		fac, ok := templates.MapFacilityBySlug(data, slug)
		if !ok {
			return templates.WebsiteErrorPage("Not Found", "no facility matching "+strconv.Quote(slug)), http.StatusNotFound, nil
		}
		return templates.WebsiteMapPopup(templates.WebsiteMapPopupParams{
			Base:     h.base(r),
			Slug:     slug,
			Facility: fac,
			Group:    group,
		}), http.StatusOK, nil
	})
}
