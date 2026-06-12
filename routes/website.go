package routes

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/a-h/templ"
	"github.com/pgaskin/ottrec-website/pkg/ottrecidx"
	"github.com/pgaskin/ottrec-website/static"
	"github.com/pgaskin/ottrec-website/templates"
)

type WebsiteConfig struct {
	Data func() (ottrecidx.DataRef, bool)
}

func Website(cfg WebsiteConfig) (http.Handler, error) {
	if cfg.Data == nil {
		return nil, fmt.Errorf("no data getter specified")
	}

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
