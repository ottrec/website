package templates

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/pgaskin/ottrec-website/exp/ottrectm"
	"github.com/pgaskin/ottrec-website/pkg/ottrecidx"
)

// This file builds the time machine trends/summary page: high-level, per-activity
// views of how much of each activity the city offers and how that has shifted
// over the loaded snapshots. The aggregation lives in exp/ottrectm
// (CategoryStats); here we turn it into small, server-rendered SVG charts.

// TimemachineTrendsParams parameterizes the trends page.
type TimemachineTrendsParams struct {
	Datasets []ottrectm.Dataset             // newest first
	Stats    [][]ottrectm.CategoryBreakdown // [snapshot][category], aligned with Datasets
	Category int                            // selected category index into ottrectm.Categories
}

// tmTrendCategory describes one entry in the category selector.
type tmTrendCategory struct {
	Slug   string
	Name   string
	Icon   string // Material Symbols glyph (borrowed from the schedule categories)
	Active bool
}

// tmTrendCategories returns the category selector entries, marking the selected
// one active and borrowing each category's icon from the schedule categories.
func tmTrendCategories(params TimemachineTrendsParams) []tmTrendCategory {
	icons := map[string]string{}
	for _, c := range ScheduleCategories {
		icons[c.Slug] = c.Icon
	}
	out := make([]tmTrendCategory, len(ottrectm.Categories))
	for i, c := range ottrectm.Categories {
		out[i] = tmTrendCategory{Slug: c.Slug, Name: c.Name, Icon: icons[c.Slug], Active: i == params.Category}
	}
	return out
}

// tmTrendName returns the display name of the selected category.
func tmTrendName(params TimemachineTrendsParams) string {
	if params.Category >= 0 && params.Category < len(ottrectm.Categories) {
		return ottrectm.Categories[params.Category].Name
	}
	return ""
}

// tmTrendHref returns the trends page URL for a category slug.
func tmTrendHref(slug string) string {
	return "/trends?activity=" + slug
}

// tmSeriesPoint is one snapshot's data point, oldest-first.
type tmSeriesPoint struct {
	t    time.Time
	vals []float64
}

// tmTrendSeries extracts the selected category's per-snapshot breakdown into
// oldest-first points, one value per band, using the supplied extractor.
func (params TimemachineTrendsParams) series(extract func(ottrectm.CategoryBreakdown) []float64) []tmSeriesPoint {
	n := len(params.Datasets)
	pts := make([]tmSeriesPoint, 0, n)
	// Datasets/Stats are newest-first; emit oldest-first for left-to-right time.
	for i := n - 1; i >= 0; i-- {
		if params.Category >= len(params.Stats[i]) {
			continue
		}
		pts = append(pts, tmSeriesPoint{
			t:    params.Datasets[i].Updated.In(ottrecidx.TZ),
			vals: extract(params.Stats[i][params.Category]),
		})
	}
	return pts
}

// latest returns the newest snapshot's breakdown for the selected category, and
// whether it exists.
func (params TimemachineTrendsParams) latest() (ottrectm.CategoryBreakdown, bool) {
	if len(params.Stats) == 0 || params.Category >= len(params.Stats[0]) {
		return ottrectm.CategoryBreakdown{}, false
	}
	return params.Stats[0][params.Category], true
}

// tmYoY holds a year-over-year comparison of total weekly hours, if the loaded
// window spans far enough back.
type tmYoY struct {
	OK       bool
	Now      float64
	Then     float64
	ThenDay  string // the date compared against
	DeltaPct float64
}

// yoy compares the newest snapshot's total weekly hours against the snapshot
// closest to one year earlier (within a month either side). OK is false if no
// snapshot is old enough.
func (params TimemachineTrendsParams) yoy() tmYoY {
	cur, ok := params.latest()
	if !ok || len(params.Datasets) == 0 {
		return tmYoY{}
	}
	target := params.Datasets[0].Updated.AddDate(-1, 0, 0)
	best := -1
	var bestDiff time.Duration
	for i := range params.Datasets {
		d := params.Datasets[i].Updated.Sub(target)
		if d < 0 {
			d = -d
		}
		if best < 0 || d < bestDiff {
			best, bestDiff = i, d
		}
	}
	if best < 0 || bestDiff > 31*24*time.Hour || params.Category >= len(params.Stats[best]) {
		return tmYoY{}
	}
	then := params.Stats[best][params.Category].Total
	var delta float64
	if then > 0 {
		delta = (cur.Total - then) / then * 100
	}
	return tmYoY{
		OK:       true,
		Now:      cur.Total,
		Then:     then,
		ThenDay:  params.Datasets[best].Updated.In(ottrecidx.TZ).Format("Jan 2, 2006"),
		DeltaPct: delta,
	}
}

// tmChart is a precomputed SVG chart: either a single filled area (Stacked) or
// several overlaid lines (one per band), plus axes. We deliberately avoid
// stacking multiple bands, since a shifting baseline makes individual series
// hard to read; each line is drawn from a common zero baseline instead.
type tmChart struct {
	W, H    int
	Bands   []tmChartBand
	XTicks  []tmTick
	YTicks  []tmTick
	Stacked bool // true: fill the (single) band as an area; false: stroke each band as a line
	Empty   bool
}

type tmChartBand struct {
	Label string
	Class string  // tm-band-N for the color
	Area  string  // SVG path "d" (a filled region when Stacked, otherwise an open line)
	Value float64 // latest value, for the legend
}

type tmTick struct {
	Pos   float64 // px along the relevant axis
	Label string
}

// chart geometry
const (
	tmChartW    = 760
	tmChartH    = 230
	tmChartPadL = 38
	tmChartPadR = 10
	tmChartPadT = 8
	tmChartPadB = 22
)

// buildChart turns oldest-first points (each with one value per band) into a
// chart. With stacked, the (single) band is filled as an area; otherwise each
// band is drawn as an independent line from a shared zero baseline.
func buildChart(pts []tmSeriesPoint, labels []string, classes []string, stacked bool) tmChart {
	c := tmChart{W: tmChartW, H: tmChartH, Stacked: stacked}
	if len(pts) < 2 {
		c.Empty = true
		return c
	}
	nb := len(labels)

	// x scale by time so gaps in the snapshot history show as gaps.
	t0 := pts[0].t
	span := pts[len(pts)-1].t.Sub(t0).Seconds()
	if span <= 0 {
		c.Empty = true
		return c
	}
	xAt := func(t time.Time) float64 {
		f := t.Sub(t0).Seconds() / span
		return tmChartPadL + f*(tmChartW-tmChartPadL-tmChartPadR)
	}

	// y scale: from the stack total when stacked, else from the largest single
	// band value (every line shares this scale and a zero baseline).
	var maxY float64
	for _, p := range pts {
		if stacked {
			var sum float64
			for _, v := range p.vals {
				sum += v
			}
			maxY = math.Max(maxY, sum)
		} else {
			for _, v := range p.vals {
				maxY = math.Max(maxY, v)
			}
		}
	}
	maxY = niceMax(maxY)
	if maxY <= 0 {
		maxY = 1
	}
	yAt := func(v float64) float64 {
		f := v / maxY
		return tmChartH - tmChartPadB - f*(tmChartH-tmChartPadT-tmChartPadB)
	}

	for b := 0; b < nb; b++ {
		var sb strings.Builder
		// top edge left→right
		for i, p := range pts {
			cmd := "L"
			if i == 0 {
				cmd = "M"
			}
			fmt.Fprintf(&sb, "%s%.1f %.1f ", cmd, xAt(p.t), yAt(bandVal(p.vals, b)))
		}
		if stacked {
			// close back along the zero baseline to fill the area
			fmt.Fprintf(&sb, "L%.1f %.1f L%.1f %.1f Z",
				xAt(pts[len(pts)-1].t), yAt(0), xAt(pts[0].t), yAt(0))
		}
		c.Bands = append(c.Bands, tmChartBand{
			Label: labels[b],
			Class: classes[b%len(classes)],
			Area:  sb.String(),
			Value: bandVal(pts[len(pts)-1].vals, b),
		})
	}

	// y ticks at 0, half, max.
	for _, v := range []float64{0, maxY / 2, maxY} {
		c.YTicks = append(c.YTicks, tmTick{Pos: yAt(v), Label: strconv.Itoa(int(math.Round(v)))})
	}
	// x ticks at ~5 evenly spaced dates.
	const nx = 5
	for i := 0; i < nx; i++ {
		f := float64(i) / float64(nx-1)
		t := t0.Add(time.Duration(f * span * float64(time.Second)))
		c.XTicks = append(c.XTicks, tmTick{Pos: xAt(t), Label: t.Format("Jan '06")})
	}
	return c
}

func bandVal(vals []float64, b int) float64 {
	if b < len(vals) {
		return vals[b]
	}
	return 0
}

// niceMax rounds v up to a clean axis maximum.
func niceMax(v float64) float64 {
	if v <= 0 {
		return 0
	}
	mag := math.Pow(10, math.Floor(math.Log10(v)))
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		if v <= m*mag {
			return m * mag
		}
	}
	return 10 * mag
}

// tmTotalChart is the headline single-series chart of total weekly hours.
func tmTotalChart(params TimemachineTrendsParams) tmChart {
	return buildChart(params.series(func(b ottrectm.CategoryBreakdown) []float64 {
		return []float64{b.Total}
	}), []string{"Total"}, []string{"tm-band-total"}, true)
}

// tmSectorChart stacks weekly hours by part of the city (skipping the unused
// "Other"/unknown sector band when it is always empty).
func tmSectorChart(params TimemachineTrendsParams) tmChart {
	idx := tmSectorBands(params)
	labels := make([]string, len(idx))
	classes := make([]string, len(idx))
	for i, si := range idx {
		labels[i] = ottrectm.SectorNames[si]
		classes[i] = "tm-band-" + strconv.Itoa(i)
	}
	return buildChart(params.series(func(b ottrectm.CategoryBreakdown) []float64 {
		out := make([]float64, len(idx))
		for i, si := range idx {
			out[i] = b.BySector[si]
		}
		return out
	}), labels, classes, false)
}

// tmSectorBands returns the sector indexes that have any hours across the loaded
// window, in display order (West, East, Central, South, then Other if nonzero).
func tmSectorBands(params TimemachineTrendsParams) []int {
	order := []int{1, 2, 3, 4, 0} // West, East, Central, South, Other (ottregions.Sector values)
	var seen [5]bool
	for _, row := range params.Stats {
		if params.Category >= len(row) {
			continue
		}
		for si := range row[params.Category].BySector {
			if row[params.Category].BySector[si] > 0 {
				seen[si] = true
			}
		}
	}
	var out []int
	for _, si := range order {
		if seen[si] {
			out = append(out, si)
		}
	}
	return out
}

// tmPeriodChart stacks weekly hours by time of day (morning/afternoon/evening).
func tmPeriodChart(params TimemachineTrendsParams) tmChart {
	return buildChart(params.series(func(b ottrectm.CategoryBreakdown) []float64 {
		return b.ByPeriod[:]
	}), ottrectm.PeriodNames[:], []string{"tm-band-0", "tm-band-2", "tm-band-3"}, false)
}

// tmWeekdayChart stacks weekly hours by weekday (Sunday first).
func tmWeekdayChart(params TimemachineTrendsParams) tmChart {
	labels := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	classes := make([]string, 7)
	for i := range classes {
		classes[i] = "tm-band-" + strconv.Itoa(i)
	}
	return buildChart(params.series(func(b ottrectm.CategoryBreakdown) []float64 {
		return b.ByWeekday[:]
	}), labels, classes, false)
}

// tmAriaCurrent returns the aria-current attribute value for the active pill.
func tmAriaCurrent(active bool) string {
	if active {
		return "page"
	}
	return "false"
}

// tmHours formats weekly hours for display.
func tmHours(v float64) string {
	return strconv.Itoa(int(math.Round(v)))
}

// tmDelta formats a signed percentage change.
func tmDelta(pct float64) string {
	sign := "+"
	if pct < 0 {
		sign = "−"
		pct = -pct
	}
	return sign + strconv.FormatFloat(pct, 'f', 0, 64) + "%"
}

// SVG rendering helpers used by the chart templ component.

func tmFmt(f float64) string { return strconv.FormatFloat(f, 'f', 1, 64) }

func (c tmChart) VB() string      { return fmt.Sprintf("0 0 %d %d", c.W, c.H) }
func (c tmChart) AxisX1() string  { return tmFmt(tmChartPadL) }
func (c tmChart) AxisX2() string  { return tmFmt(tmChartW - tmChartPadR) }
func (c tmChart) XLabelY() string { return tmFmt(tmChartH - tmChartPadB + 14) }
func (c tmChart) YLabelX() string { return tmFmt(tmChartPadL - 5) }

func (t tmTick) PosStr() string { return tmFmt(t.Pos) }

// tmDeltaClass returns the badge class for a YoY delta.
func tmDeltaClass(pct float64) string {
	switch {
	case pct > 0.5:
		return "tm-added"
	case pct < -0.5:
		return "tm-removed"
	default:
		return "tm-modified"
	}
}
