// Command ottrec-timemachine serves historical snapshots of ottrec data.
//
// Unlike ottrec-website, which serves the single latest dataset polled from
// data.ottrec.ca, ottrec-timemachine loads many historical datasets directly
// out of the ottrec-data cache database (shared read-only with the ottrec-data
// process) and keeps them all in memory for browsing back in time.
package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"
	_ "time/tzdata"

	"github.com/a-h/templ"
	"github.com/lmittmann/tint"
	"github.com/pgaskin/ottrec-website/exp/ottrectm"
	"github.com/pgaskin/ottrec-website/internal/asset"
	"github.com/pgaskin/ottrec-website/internal/pflagx"
	"github.com/pgaskin/ottrec-website/pkg/ottrecidx"
	"github.com/pgaskin/ottrec-website/routes"
	"github.com/pgaskin/ottrec-website/static"
	"github.com/pgaskin/ottrec-website/templates"
	"github.com/spf13/pflag"
)

var (
	EnvPrefix = "OTTREC_TIMEMACHINE_"
	Addr      = pflag.StringP("addr", "a", ":8084", "listen address")
	Cache     = pflag.StringP("cache", "c", "/tmp/ottrec-data.db", "path to the ottrec-data cache database (opened read-only)")
	MaxAge    = pflag.IntP("max-age", "m", 730, "maximum age of data to load, in days")
	LogLevel  = pflagx.LevelP("log-level", "L", slog.LevelInfo, "log level")
	LogJSON   = pflag.Bool("log-json", false, "use json logs")
	HeadHTML  = pflag.String("head-html", "", "raw html to inject at the bottom of <head> on every page")
	Help      = pflag.BoolP("help", "h", false, "show this help text")

	// render-to-file mode: write a single self-contained HTML page (with the
	// stylesheets inlined) instead of starting the server, for quick inspection
	// or a headless screenshot.
	Render   = pflag.StringP("render", "r", "", "render a page to a file instead of serving: one of \"index\", \"facilities\", \"trends\", \"diff\", \"facility\"")
	Out      = pflag.StringP("out", "o", "/tmp/timemachine.html", "output file for --render")
	From     = pflag.String("from", "", "older snapshot for --render diff (YYYY-MM-DD or index, newest=0)")
	To       = pflag.String("to", "", "newer snapshot for --render diff (YYYY-MM-DD or index, newest=0)")
	Facility = pflag.String("facility", "", "facility slug for --render facility")
)

// TODO: http logs, request id

func main() {
	if val, ok := os.LookupEnv("PORT"); ok {
		if err := pflag.Set("addr", ":"+val); err != nil {
			panic(err)
		}
	}
	pflagx.ParseEnv(EnvPrefix)
	pflag.Parse()

	if *Help || pflag.NArg() != 0 {
		fmt.Printf("usage: %s [options]\n%s", os.Args[0], pflag.CommandLine.FlagUsages())
		if *Help {
			return
		}
		os.Exit(2)
	}

	if *Cache == "" {
		fmt.Fprintf(os.Stderr, "error: no cache path specified\n")
		os.Exit(2)
	}
	if *MaxAge < 0 {
		fmt.Fprintf(os.Stderr, "error: max-age must not be negative\n")
		os.Exit(2)
	}

	if *LogJSON {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: LogLevel,
		})))
	} else {
		slog.SetDefault(slog.New(tint.NewHandler(os.Stdout, &tint.Options{
			Level: LogLevel,
		})))
	}
	slog.SetLogLoggerLevel(LogLevel.Level())

	if err := run(); err != nil {
		slog.Error("failed to run server", "error", err)
		os.Exit(1)
	}
}

func run() error {
	st, err := ottrectm.Open(context.Background(), *Cache, time.Duration(*MaxAge)*24*time.Hour)
	if err != nil {
		return err
	}
	defer st.Close()

	if *Render != "" {
		templates.SetHeadExtra(*HeadHTML)
		return renderFile(st)
	}

	handler, err := routes.Timemachine(routes.TimemachineConfig{
		Datasets:      st.Datasets,
		Magnitudes:    st.Magnitudes,
		FacilityStats: st.FacilityStats,
		CategoryStats: st.CategoryStats,
		HeadHTML:      *HeadHTML,
	})
	if err != nil {
		return fmt.Errorf("initialize routes: %w", err)
	}

	slog.Info("http: listening", "addr", *Addr)
	return http.ListenAndServe(*Addr, handler)
}

// renderFile renders one page to a self-contained HTML file (with the
// stylesheets inlined) for inspection or a headless screenshot.
func renderFile(st *ottrectm.Store) error {
	sets := st.Datasets()
	if len(sets) == 0 {
		return fmt.Errorf("no datasets loaded")
	}

	var c templ.Component
	switch *Render {
	case "index":
		c = templates.TimemachineIndexPage(templates.TimemachineIndexParams{
			Datasets: sets,
		})
	case "facilities":
		c = templates.TimemachineFacilitiesPage(templates.TimemachineFacilitiesParams{
			Data:  sets[0].Data,
			Slugs: templates.TimemachineSlugs(sets[0].Data),
			Stats: st.FacilityStats(),
		})
	case "diff":
		if len(sets) < 2 {
			return fmt.Errorf("need at least two snapshots to diff")
		}
		newDS, err := selectDataset(sets, *To, 0)
		if err != nil {
			return fmt.Errorf("select --to: %w", err)
		}
		oldDS, err := selectDataset(sets, *From, 1)
		if err != nil {
			return fmt.Errorf("select --from: %w", err)
		}
		slog.Info("rendering diff", "from", tmDay(oldDS.Updated), "to", tmDay(newDS.Updated))
		c = templates.TimemachineDiffPage(templates.TimemachineDiffParams{
			Datasets:   sets,
			Magnitudes: st.Magnitudes(),
			Old:        oldDS,
			New:        newDS,
			Diff:       ottrectm.Compare(oldDS.Data, newDS.Data),
			Slugs:      templates.TimemachineSlugs(newDS.Data, oldDS.Data),
		})
	case "trends":
		cat := 0
		if *Facility != "" {
			if i := ottrectm.CategoryBySlug(*Facility); i >= 0 {
				cat = i
			} else {
				return fmt.Errorf("unknown activity slug %q for --facility", *Facility)
			}
		}
		slog.Info("rendering trends", "activity", ottrectm.Categories[cat].Slug)
		c = templates.TimemachineTrendsPage(templates.TimemachineTrendsParams{
			Datasets: sets,
			Stats:    st.CategoryStats(),
			Category: cat,
			Heatmap:  sets[0].CategoryHeatmap(cat),
		})
	case "facility":
		if *Facility == "" {
			return fmt.Errorf("--facility slug required")
		}
		key, name, url, current, ok := templates.TimemachineResolveFacility(sets, *Facility)
		if !ok {
			return fmt.Errorf("no facility matching slug %q", *Facility)
		}
		entries := ottrectm.FacilityTimeline(sets, key)
		slog.Info("rendering facility timeline", "name", name, "changes", len(entries))
		c = templates.TimemachineFacilityPage(templates.TimemachineFacilityParams{
			Slug:     *Facility,
			Name:     name,
			URL:      url,
			Current:  current,
			Entries:  entries,
			Datasets: sets,
		})
	default:
		return fmt.Errorf("unknown --render mode %q (want index, facilities, trends, diff, or facility)", *Render)
	}

	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		return fmt.Errorf("render: %w", err)
	}
	out := inlineStyles(buf.String())
	if err := os.WriteFile(*Out, []byte(out), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", *Out, err)
	}
	slog.Info("wrote page", "file", *Out, "bytes", len(out))
	return nil
}

// selectDataset resolves a snapshot selector (a YYYY-MM-DD date, a numeric index
// with newest=0, or "" for the default index) to a dataset.
func selectDataset(sets []ottrectm.Dataset, sel string, def int) (ottrectm.Dataset, error) {
	if sel == "" {
		return sets[def], nil
	}
	if i, err := strconv.Atoi(sel); err == nil {
		if i < 0 || i >= len(sets) {
			return ottrectm.Dataset{}, fmt.Errorf("index %d out of range (have %d)", i, len(sets))
		}
		return sets[i], nil
	}
	for _, ds := range sets {
		if tmDay(ds.Updated) == sel {
			return ds, nil
		}
	}
	return ottrectm.Dataset{}, fmt.Errorf("no snapshot for %q", sel)
}

func tmDay(t time.Time) string {
	return t.In(ottrecidx.TZ).Format("2006-01-02")
}

// inlineStyles replaces the page's stylesheet <link>s with inline <style>
// blocks, so the output file renders without a server.
func inlineStyles(s string) string {
	for _, a := range []*asset.Asset{static.WebsiteCSS, static.FacilityCSS, static.TimemachineCSS} {
		p := static.Path(a)
		css := static.InlineCSS(a)
		re := regexp.MustCompile(`<link[^>]*href="` + regexp.QuoteMeta(p) + `"[^>]*>`)
		s = re.ReplaceAllStringFunc(s, func(string) string { return "<style>" + css + "</style>" })
	}
	return s
}
