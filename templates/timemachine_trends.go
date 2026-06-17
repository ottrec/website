package templates

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/ottrec/website/exp/ottrectm"
	"github.com/ottrec/website/pkg/ottrecidx"
)

// This file builds the time machine trends/summary page: high-level, per-activity
// views of how much of each activity the city offers and how that has shifted
// over the loaded snapshots. The aggregation lives in exp/ottrectm
// (CategoryStats); here we turn it into small, server-rendered SVG charts.

// TimemachineTrendsParams parameterizes the trends page.
type TimemachineTrendsParams struct {
	Datasets []ottrectm.Dataset               // newest first
	Stats    [][]ottrectm.CategoryBreakdown   // [snapshot][category], aligned with Datasets
	Category int                              // selected category index into ottrectm.Categories
	Heatmap  [7][ottrectm.HeatmapBins]float64 // newest snapshot's weekday × half-hour availability for the category
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

// tmChart is a precomputed SVG chart: the first Fill bands are drawn as filled
// areas from a zero baseline, the rest as overlaid lines. We deliberately avoid
// cumulative stacking, since a shifting baseline makes individual series hard to
// read; every band shares one zero baseline. Points carries the underlying data
// (via DataJSON) so the chart can be made hoverable client-side.
type tmChart struct {
	W, H   int
	Bands  []tmChartBand
	XTicks []tmTick
	YTicks []tmTick
	Points []tmChartPoint
	Unit   string // value unit for hover tooltips (e.g. "h", "h/facility")
	Empty  bool
}

type tmChartBand struct {
	Label string
	Class string  // tm-band-N for the color
	Area  string  // SVG path "d" (a filled region when Fill, otherwise an open line)
	Fill  bool    // render as a filled area rather than a stroked line
	Value float64 // latest value, for the legend
}

// tmChartPoint is one snapshot's plotted data point, for the hover tooltip.
type tmChartPoint struct {
	X    float64   `json:"x"`
	Date string    `json:"date"`
	Vals []float64 `json:"vals"`
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
// chart. The first fill bands are filled as areas from zero; the remaining bands
// are stroked as lines. All bands share a y-scale based on the largest single
// band value, so any series can be read against the axis directly.
func buildChart(pts []tmSeriesPoint, labels []string, classes []string, fill int, unit string) tmChart {
	c := tmChart{W: tmChartW, H: tmChartH, Unit: unit}
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

	// y scale from the largest single band value (every band shares it).
	var maxY float64
	for _, p := range pts {
		for _, v := range p.vals {
			maxY = math.Max(maxY, v)
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
		isFill := b < fill
		if isFill {
			// close back along the zero baseline to fill the area
			fmt.Fprintf(&sb, "L%.1f %.1f L%.1f %.1f Z",
				xAt(pts[len(pts)-1].t), yAt(0), xAt(pts[0].t), yAt(0))
		}
		c.Bands = append(c.Bands, tmChartBand{
			Label: labels[b],
			Class: classes[b%len(classes)],
			Area:  sb.String(),
			Fill:  isFill,
			Value: bandVal(pts[len(pts)-1].vals, b),
		})
	}

	// per-snapshot points for the hover tooltip.
	for _, p := range pts {
		vals := make([]float64, nb)
		for b := range vals {
			vals[b] = math.Round(bandVal(p.vals, b)*10) / 10
		}
		c.Points = append(c.Points, tmChartPoint{X: math.Round(xAt(p.t)*10) / 10, Date: p.t.Format("Jan 2, 2006"), Vals: vals})
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

// DataJSON returns the chart's data and plot geometry as JSON, embedded for the
// client-side hover handler in timemachine.ts.
func (c tmChart) DataJSON() string {
	type band struct {
		Label string `json:"label"`
		Class string `json:"class"`
	}
	bands := make([]band, len(c.Bands))
	for i, b := range c.Bands {
		bands[i] = band{Label: b.Label, Class: b.Class}
	}
	v, err := json.Marshal(struct {
		W      int            `json:"w"`
		PlotL  float64        `json:"plotL"`
		PlotR  float64        `json:"plotR"`
		Top    float64        `json:"top"`
		Bottom float64        `json:"bottom"`
		Unit   string         `json:"unit"`
		Bands  []band         `json:"bands"`
		Points []tmChartPoint `json:"points"`
	}{
		W:      c.W,
		PlotL:  tmChartPadL,
		PlotR:  tmChartW - tmChartPadR,
		Top:    tmChartPadT,
		Bottom: tmChartH - tmChartPadB,
		Unit:   c.Unit,
		Bands:  bands,
		Points: c.Points,
	})
	if err != nil {
		return "{}"
	}
	return string(v)
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

// tmTotalChart is the headline chart: total weekly hours as a filled area with
// the accessible (evenings + weekends) portion drawn as a line inside it, so the
// gap up to the top edge is the part locked behind the Mon–Fri 9–5 workday.
func tmTotalChart(params TimemachineTrendsParams) tmChart {
	return buildChart(params.series(func(b ottrectm.CategoryBreakdown) []float64 {
		return []float64{b.Total, b.Accessible}
	}), []string{"Total", "Outside 9–5"}, []string{"tm-band-total", "tm-band-2"}, 1, "h")
}

// tmPerFacilityChart shows the average weekly hours offered per facility, a
// measure of intensity that separates "more facilities" from "more hours each".
func tmPerFacilityChart(params TimemachineTrendsParams) tmChart {
	return buildChart(params.series(func(b ottrectm.CategoryBreakdown) []float64 {
		return []float64{b.PerFacility()}
	}), []string{"Hours per facility"}, []string{"tm-band-1"}, 1, "h/facility")
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
	}), labels, classes, 0, "h")
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
	}), ottrectm.PeriodNames[:], []string{"tm-band-0", "tm-band-2", "tm-band-3"}, 0, "h")
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
	}), labels, classes, 0, "h")
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

// tmAccessiblePct formats the share of hours outside the 9–5 workday as a
// percentage.
func tmAccessiblePct(b ottrectm.CategoryBreakdown) string {
	if b.Total <= 0 {
		return "—"
	}
	return strconv.Itoa(int(math.Round(b.Accessible/b.Total*100))) + "%"
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

// tmHeat is a precomputed weekday × half-hour availability heatmap for the
// newest snapshot, trimmed to the occupied hours.
type tmHeat struct {
	Empty bool
	Max   float64
	Cols  []tmHeatCol // one per shown half-hour, left→right
	Rows  []tmHeatRow // one per weekday, Sunday→Saturday
}

type tmHeatCol struct {
	Hour string // hour label shown on the hour ("" otherwise)
}

type tmHeatRow struct {
	Day   string
	Cells []tmHeatCell
}

type tmHeatCell struct {
	Op    string // fill opacity (empty for no fill)
	Title string // hover title
}

var tmHeatDays = [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// tmHeatmap turns the newest snapshot's weekday × half-hour matrix into a
// renderable heatmap, trimmed to the range of half-hours with any availability.
func tmHeatmap(params TimemachineTrendsParams) tmHeat {
	m := params.Heatmap
	var max float64
	lo, hi := -1, -1
	for d := range m {
		for b := range m[d] {
			if m[d][b] > 0 {
				if lo < 0 || b < lo {
					lo = b
				}
				if b > hi {
					hi = b
				}
				max = math.Max(max, m[d][b])
			}
		}
	}
	if lo < 0 || max <= 0 {
		return tmHeat{Empty: true}
	}
	// pad to whole hours for tidy labels.
	lo -= lo % 2
	if hi%2 == 0 {
		hi++
	}
	h := tmHeat{Max: max}
	for b := lo; b <= hi; b++ {
		hour := ""
		if b%2 == 0 {
			hour = strconv.Itoa((b * 30) / 60)
		}
		h.Cols = append(h.Cols, tmHeatCol{Hour: hour})
	}
	for d := range m {
		row := tmHeatRow{Day: tmHeatDays[d]}
		for b := lo; b <= hi; b++ {
			var cell tmHeatCell
			if v := m[d][b]; v > 0 {
				// floor the opacity so faint cells stay visible.
				cell.Op = strconv.FormatFloat(0.18+0.82*v/max, 'f', 2, 64)
				cell.Title = fmt.Sprintf("%s %s — %s h", tmHeatDays[d], tmHeatClock(b), tmHours1(v))
			}
			row.Cells = append(row.Cells, cell)
		}
		h.Rows = append(h.Rows, row)
	}
	return h
}

// tmHeatClock formats a half-hour bin index as a HH:MM clock label.
func tmHeatClock(bin int) string {
	mins := bin * 30
	return fmt.Sprintf("%d:%02d", mins/60, mins%60)
}

// tmHours1 formats hours with one decimal place.
func tmHours1(v float64) string {
	return strconv.FormatFloat(v, 'f', 1, 64)
}
