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
	"github.com/pgaskin/ottrec-website/pkg/ottrecidx"
	"github.com/pgaskin/ottrec-website/pkg/ottrecql"
	"github.com/pgaskin/ottrec-website/static"
	"github.com/pgaskin/ottrec-website/templates"
)

type WebsiteConfig struct {
	Data func() (ottrecidx.DataRef, bool)
	// HeadHTML is raw HTML injected at the bottom of <head> on every page.
	HeadHTML  string
	AboutHTML string
}

func Website(cfg WebsiteConfig) (http.Handler, error) {
	if cfg.Data == nil {
		return nil, fmt.Errorf("no data getter specified")
	}
	templates.SetHeadExtra(cfg.HeadHTML)
	templates.SetAboutExtra(cfg.AboutHTML)

	base := websiteHandlerBase{
		Data: cfg.Data,
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
	mux.Handle("GET /about", &websiteAboutHandler{
		websiteHandlerBase: base,
	})
	mux.Handle("/static/", static.Handler(static.Website))
	mux.Handle("GET /favicon.ico", static.Handler(static.Website))

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
	Host string
	Data func() (ottrecidx.DataRef, bool)
}

func (h *websiteHandlerBase) render(w http.ResponseWriter, r *http.Request, fn func(data ottrecidx.DataRef) (c templ.Component, status int, err error)) {
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
	if err := templates.Render(w, r, templates.WebsiteErrorPage, data.Index().Hash(), func() (c templ.Component, status int, err error) {
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

	paths := []string{"", "map", "schedules", "activities", "about"}
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

	etag := etagWeak(data.Index().Hash())
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, no-cache")
	w.Header().Set("ETag", etag)
	if slices.Contains(r.Header.Values("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
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

// websiteMapFacilityHandler serves the HTML fragment fetched over XHR for the
// map facility popups.
type websiteMapFacilityHandler struct {
	websiteHandlerBase
}

func (h *websiteMapFacilityHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Accept-Encoding")
	w.Header().Set("Cache-Control", "public, no-cache")

	if r.URL.RawQuery != "" {
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
		return templates.WebsiteMapPopup(templates.WebsiteMapPopupParams{
			Base:     h.base(r),
			Slug:     slug,
			Facility: fac,
		}), http.StatusOK, nil
	})
}
