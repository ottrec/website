package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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
	HeadHTML string
}

func Website(cfg WebsiteConfig) (http.Handler, error) {
	if cfg.Data == nil {
		return nil, fmt.Errorf("no data getter specified")
	}
	templates.SetHeadExtra(cfg.HeadHTML)

	base := websiteHandlerBase{
		Data: cfg.Data,
	}
	mux := http.NewServeMux()

	// TODO: favicon
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
	mux.Handle("GET /api/ottrecql/validate", &websiteOttrecqlValidateHandler{})
	mux.Handle("GET /about", &websiteAboutHandler{
		websiteHandlerBase: base,
	})
	mux.Handle("/static/", static.Handler(static.Website))

	return commonMiddleware(mux), nil
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

	if r.URL.RawQuery != "" {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, r.URL.EscapedPath(), http.StatusTemporaryRedirect)
		return
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
	if r.URL.RawQuery != "" && q == "" && !advanced {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, r.URL.EscapedPath(), http.StatusTemporaryRedirect)
		return
	}

	h.render(w, r, func(data ottrecidx.DataRef) (templ.Component, int, error) {
		params := templates.WebsiteSchedulesParams{
			Base:        h.base(r),
			Data:        data,
			Canonical:   "schedules",
			Active:      "all",
			Title:       "Schedules",
			Description: "Drop-in schedules across all City of Ottawa recreation facilities.",
			Search:      true,
			Advanced:    advanced,
			Query:       q,
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

// websiteSchedulesKeyHandler serves the category and single-facility schedule
// pages under /schedules/.
type websiteSchedulesKeyHandler struct {
	websiteHandlerBase
}

func (h *websiteSchedulesKeyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Accept-Encoding")
	w.Header().Set("Cache-Control", "public, no-cache")

	if r.URL.RawQuery != "" {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, r.URL.EscapedPath(), http.StatusTemporaryRedirect)
		return
	}

	key := r.PathValue("key")
	h.render(w, r, func(data ottrecidx.DataRef) (templ.Component, int, error) {
		params := templates.WebsiteSchedulesParams{
			Base: h.base(r),
			Data: data,
		}
		if cat, ok := templates.ScheduleCategoryBySlug(key); ok {
			filtered, err := templates.SchedulesFilter(data, cat.Query())
			if err != nil {
				return nil, 0, err
			}
			params.Canonical = "schedules/" + cat.Slug
			params.Active = cat.Slug
			params.Title = cat.Name + " Schedules"
			params.Description = cat.Description
			params.CategoryTerms = cat.Activities
			params.TOC = templates.SchedulesTOC(filtered, templates.MapFacilitySlugger(data))
		} else if fac, ok := templates.MapFacilityBySlug(data, key); ok {
			params.Canonical = "schedules"
			params.Title = fac.GetName()
			params.Description = "Drop-in schedules for " + fac.GetName() + "."
			params.Single = true
			params.TOC = templates.SchedulesFacilityTOC(key, fac)
		} else {
			return templates.WebsiteErrorPage("Not Found", "no schedule category or facility matching "+strconv.Quote(key)), http.StatusNotFound, nil
		}
		return templates.WebsiteSchedulesPage(params), http.StatusOK, nil
	})
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
