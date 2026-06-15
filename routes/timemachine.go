package routes

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
	"github.com/pgaskin/ottrec-website/exp/ottrectm"
	"github.com/pgaskin/ottrec-website/static"
	"github.com/pgaskin/ottrec-website/templates"
)

type TimemachineConfig struct {
	// Datasets returns the currently loaded datasets, newest first.
	Datasets func() []ottrectm.Dataset
	// Magnitudes returns the cached per-snapshot change magnitudes, aligned with
	// Datasets (see ottrectm.Magnitudes). Optional; computed on demand if nil.
	Magnitudes func() []int
	// FacilityStats returns the cached per-facility change stats (see
	// ottrectm.FacilityChangeStats). Optional; computed on demand if nil.
	FacilityStats func() map[string]ottrectm.FacilityStats
	// CategoryStats returns the cached per-snapshot per-category breakdowns (see
	// ottrectm.CategoryStats). Optional; computed on demand if nil.
	CategoryStats func() [][]ottrectm.CategoryBreakdown
	// HeadHTML is raw HTML injected at the bottom of <head> on every page.
	HeadHTML string
}

func Timemachine(cfg TimemachineConfig) (http.Handler, error) {
	if cfg.Datasets == nil {
		return nil, fmt.Errorf("no datasets getter specified")
	}
	templates.SetHeadExtra(cfg.HeadHTML)

	base := timemachineBase{Datasets: cfg.Datasets, Magnitudes: cfg.Magnitudes, FacilityStats: cfg.FacilityStats, CategoryStats: cfg.CategoryStats}
	mux := http.NewServeMux()
	mux.Handle("GET /{$}", &timemachineIndexHandler{base})
	mux.Handle("GET /datasets", &timemachineDiffHandler{base})
	mux.Handle("GET /facilities", &timemachineFacilitiesHandler{base})
	mux.Handle("GET /facility/{slug}", &timemachineFacilityHandler{base})
	mux.Handle("GET /trends", &timemachineTrendsHandler{base})
	mux.Handle("GET /robots.txt", http.HandlerFunc(timemachineRobots))
	mux.Handle("/static/", static.Handler(static.Website))
	mux.Handle("GET /favicon.ico", static.Handler(static.Website))

	return commonMiddleware(mux), nil
}

type timemachineBase struct {
	Datasets      func() []ottrectm.Dataset
	Magnitudes    func() []int
	FacilityStats func() map[string]ottrectm.FacilityStats
	CategoryStats func() [][]ottrectm.CategoryBreakdown
}

// categoryStats returns the per-snapshot per-category breakdowns for sets,
// using the cached getter when available and falling back to computing them.
func (b timemachineBase) categoryStats(sets []ottrectm.Dataset) [][]ottrectm.CategoryBreakdown {
	if b.CategoryStats != nil {
		if m := b.CategoryStats(); len(m) == len(sets) {
			return m
		}
	}
	return ottrectm.CategoryStats(sets)
}

// facilityStats returns the per-facility change stats, using the cached getter
// when available and falling back to computing them.
func (b timemachineBase) facilityStats(sets []ottrectm.Dataset) map[string]ottrectm.FacilityStats {
	if b.FacilityStats != nil {
		if m := b.FacilityStats(); m != nil {
			return m
		}
	}
	return ottrectm.FacilityChangeStats(sets)
}

// magnitudes returns the per-snapshot change magnitudes for sets, using the
// cached getter when available and falling back to computing them.
func (b timemachineBase) magnitudes(sets []ottrectm.Dataset) []int {
	if b.Magnitudes != nil {
		if m := b.Magnitudes(); len(m) == len(sets) {
			return m
		}
	}
	return ottrectm.Magnitudes(sets)
}

// datasetByID returns the dataset with the given id, or false.
func (b timemachineBase) datasetByID(sets []ottrectm.Dataset, id string) (ottrectm.Dataset, bool) {
	for _, ds := range sets {
		if ds.ID == id {
			return ds, true
		}
	}
	return ottrectm.Dataset{}, false
}

// timemachineRender renders a component to the client, buffering first so a
// render error doesn't corrupt a partial response.
func timemachineRender(w http.ResponseWriter, r *http.Request, c templ.Component, status int) {
	var buf bytes.Buffer
	if err := c.Render(r.Context(), &buf); err != nil {
		slog.Error("timemachine: failed to render", "url", r.URL.String(), "error", err)
		http.Error(w, "internal server error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

type timemachineIndexHandler struct{ timemachineBase }

func (h *timemachineIndexHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sets := h.Datasets()
	if len(sets) == 0 {
		http.Error(w, "no datasets loaded", http.StatusServiceUnavailable)
		return
	}
	timemachineRender(w, r, templates.TimemachineIndexPage(templates.TimemachineIndexParams{
		Datasets: sets,
	}), http.StatusOK)
}

type timemachineFacilitiesHandler struct{ timemachineBase }

func (h *timemachineFacilitiesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sets := h.Datasets()
	if len(sets) == 0 {
		http.Error(w, "no datasets loaded", http.StatusServiceUnavailable)
		return
	}
	timemachineRender(w, r, templates.TimemachineFacilitiesPage(templates.TimemachineFacilitiesParams{
		Data:  sets[0].Data,
		Slugs: templates.TimemachineSlugs(sets[0].Data),
		Stats: h.facilityStats(sets),
	}), http.StatusOK)
}

type timemachineDiffHandler struct{ timemachineBase }

func (h *timemachineDiffHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sets := h.Datasets()
	if len(sets) < 2 {
		http.Error(w, "not enough datasets loaded to diff", http.StatusServiceUnavailable)
		return
	}

	// default to the two newest snapshots.
	newDS, oldDS := sets[0], sets[1]
	if id := r.URL.Query().Get("to"); id != "" {
		if ds, ok := h.datasetByID(sets, id); ok {
			newDS = ds
		}
	}
	if id := r.URL.Query().Get("from"); id != "" {
		if ds, ok := h.datasetByID(sets, id); ok {
			oldDS = ds
		}
	}

	params := templates.TimemachineDiffParams{
		Datasets: sets,
		Old:      oldDS,
		New:      newDS,
		Diff:     ottrectm.Compare(oldDS.Data, newDS.Data),
		Slugs:    templates.TimemachineSlugs(newDS.Data, oldDS.Data),
	}

	// "only" mode restricts the diff (and the overview bars) to one facility.
	if slug := r.URL.Query().Get("only"); slug != "" {
		key, name, _, _, ok := templates.TimemachineResolveFacility(sets, slug)
		if ok {
			params.OnlySlug = slug
			params.OnlyName = name
			params.Diff = filterDiffFacility(params.Diff, key)
			params.Magnitudes = ottrectm.MagnitudesFacility(sets, key)
		}
	}
	if params.Magnitudes == nil {
		params.Magnitudes = h.magnitudes(sets)
	}

	timemachineRender(w, r, templates.TimemachineDiffPage(params), http.StatusOK)
}

// filterDiffFacility narrows a data diff to the single facility with the given
// key (source URL).
func filterDiffFacility(d *ottrectm.DataDiff, key string) *ottrectm.DataDiff {
	out := &ottrectm.DataDiff{}
	for _, f := range d.Facilities {
		if f.URL == key {
			out.Facilities = append(out.Facilities, f)
		}
	}
	return out
}

// timemachineRobots disallows everything except the homepage (only the homepage
// is indexable; the rest set noindex too).
func timemachineRobots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	io.WriteString(w, "User-agent: *\nAllow: /$\nDisallow: /\n")
}

type timemachineFacilityHandler struct{ timemachineBase }

func (h *timemachineFacilityHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sets := h.Datasets()
	if len(sets) == 0 {
		http.Error(w, "no datasets loaded", http.StatusServiceUnavailable)
		return
	}

	slug := r.PathValue("slug")
	key, name, url, current, ok := templates.TimemachineResolveFacility(sets, slug)
	if !ok {
		http.Error(w, "no facility matching "+slug, http.StatusNotFound)
		return
	}

	timemachineRender(w, r, templates.TimemachineFacilityPage(templates.TimemachineFacilityParams{
		Slug:     slug,
		Name:     name,
		URL:      url,
		Current:  current,
		Entries:  ottrectm.FacilityTimeline(sets, key),
		Datasets: sets,
	}), http.StatusOK)
}

type timemachineTrendsHandler struct{ timemachineBase }

func (h *timemachineTrendsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sets := h.Datasets()
	if len(sets) == 0 {
		http.Error(w, "no datasets loaded", http.StatusServiceUnavailable)
		return
	}

	// default to the first category (swimming).
	cat := 0
	if slug := r.URL.Query().Get("activity"); slug != "" {
		if i := ottrectm.CategoryBySlug(slug); i >= 0 {
			cat = i
		}
	}

	timemachineRender(w, r, templates.TimemachineTrendsPage(templates.TimemachineTrendsParams{
		Datasets: sets,
		Stats:    h.categoryStats(sets),
		Category: cat,
	}), http.StatusOK)
}
