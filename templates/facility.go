package templates

import (
	"fmt"
	"iter"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/ottrec/scraper/schema"
	"github.com/ottrec/website/pkg/ottrecidx"
	"github.com/ottrec/website/pkg/ottregions"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// WebsiteFacilityArticleOptions controls the optional parts of
// [WebsiteFacilityArticle].
type WebsiteFacilityArticleOptions struct {
	// Anchors, if non-empty, gives the schedule groups ids from
	// [facilityAnchorID].
	Anchors string
	// Link, if non-empty, makes the facility name link to it.
	Link string
	// HeaderLinks replaces the address section with small Directions and City
	// of Ottawa links on the right side of the header.
	HeaderLinks bool
	// List renders the schedules as compact per-activity day/time lists
	// instead of tables, eliding the group titles and schedule captions, and
	// makes the special hours/notifications/changes sections collapsible.
	List bool
	// Star, if non-empty, is the facility slug for a star toggle button next
	// to the facility name (hidden until starred.js reveals it).
	Star string
	// Group, if non-nil, restricts the rendered schedules to the schedule group
	// at that index (in document order); the other facility sections are
	// unaffected. Out-of-range values render no groups.
	Group *int
	// Region shows the facility's region and sector under its name (the map
	// popups omit it since the location is already visible on the map).
	Region bool
}

// facilityDirectionsURL returns a Google Maps directions link for the
// facility, or an empty string if there's nothing to link to.
func facilityDirectionsURL(fac ottrecidx.FacilityRef) string {
	if addr := mapOneLineAddress(fac.GetAddress()); addr != "" {
		return "https://www.google.com/maps/dir/?api=1&destination=" + url.QueryEscape(addr)
	}
	if lng, lat, ok := fac.GetLngLat(); ok {
		return "https://www.google.com/maps/dir/?api=1&destination=" +
			strconv.FormatFloat(float64(lat), 'f', -1, 32) + "," +
			strconv.FormatFloat(float64(lng), 'f', -1, 32)
	}
	return ""
}

// facilityRegionLabel returns the facility's region and sector for display
// (e.g. "Barrhaven · South"), or an empty string if it has no known region.
func facilityRegionLabel(fac ottrecidx.FacilityRef) string {
	r := fac.Region()
	if r == ottregions.RegionUnknown {
		return ""
	}
	return r.Name() + " · " + fac.Sector().String()
}

// indexedSeq numbers a sequence, for stable anchor ids.
func indexedSeq[T any, S ~func(func(T) bool)](seq S) iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		var i int
		for v := range seq {
			if !yield(i, v) {
				return
			}
			i++
		}
	}
}

func activityLabel(act ottrecidx.ActivityRef) string {
	if s := act.GetName(); s != "" {
		return s
	}
	return act.GetLabel()
}

func timeLabel(tm ottrecidx.TimeRef) string {
	if r, ok := tm.GetRange(); ok {
		return clockRangeLabel(r)
	}
	return tm.GetLabel()
}

func clockRangeLabel(r schema.ClockRange) string {
	_, sh, sm := r.Start.Split()
	_, eh, em := r.End.Split()

	sdh := sh % 12
	if sdh == 0 {
		sdh = 12
	}
	edh := eh % 12
	if edh == 0 {
		edh = 12
	}
	esuf := "am"
	if eh >= 12 {
		esuf = "pm"
	}

	var st string
	if (sh < 12) == (eh < 12) {
		st = fmt.Sprintf("%d:%02d", sdh, sm)
	} else {
		ssuf := "am"
		if sh >= 12 {
			ssuf = "pm"
		}
		st = fmt.Sprintf("%d:%02d%s", sdh, sm, ssuf)
	}
	return st + "–" + fmt.Sprintf("%d:%02d%s", edh, em, esuf)
}

func scheduleDateRangeLabel(sch ottrecidx.ScheduleRef) string {
	er, ok := sch.ComputeEffectiveDateRange()
	if !ok {
		return sch.GetDate()
	}
	format := func(d schema.Date) string {
		if t, ok := d.GoTime(ottrecidx.TZ); ok {
			return t.Format("Jan 2, 2006")
		}
		return d.String()
	}
	switch {
	case er.From.IsZero() && er.To.IsZero():
		return ""
	case er.From.IsZero():
		return "until " + format(er.To)
	case er.To.IsZero():
		return "from " + format(er.From)
	default:
		return format(er.From) + " to " + format(er.To)
	}
}

// scheduleClass returns the table classes for a schedule, marking it as past
// or future relative to the current date so it can be dimmed (like
// pkg/ottrecexph does client-side). Note that since this is rendered
// server-side, a cached page may classify schedules as of when it was
// rendered, but it self-corrects whenever the data updates (daily).
func scheduleClass(sch ottrecidx.ScheduleRef) string {
	er, ok := sch.ComputeEffectiveDateRange()
	if !ok {
		return "schedule"
	}
	// strip the weekday digit so dates compare at day granularity
	today := schema.MakeDateFromGo(time.Now().In(ottrecidx.TZ)) / 10
	if !er.From.IsZero() && today < er.From/10 {
		return "schedule schedule-future"
	}
	if !er.To.IsZero() && today > er.To/10 {
		return "schedule schedule-past"
	}
	return "schedule"
}

// scheduleListDayLabel returns the compact label for the i'th day of a
// schedule in the list view, abbreviating leading weekday names.
func scheduleListDayLabel(sch ottrecidx.ScheduleRef, i int) string {
	var s string
	if d, ok := sch.GetDayDate(i); ok {
		s = d.String()
	} else {
		s = sch.GetDay(i)
	}
	for wd := time.Sunday; wd <= time.Saturday; wd++ {
		if name := wd.String(); strings.HasPrefix(s, name) {
			return name[:3] + s[len(name):]
		}
	}
	return s
}

func activityHasTimes(act ottrecidx.ActivityRef) bool {
	for range act.Times() {
		return true
	}
	return false
}

func activityHasDayTimes(act ottrecidx.ActivityRef, i int) bool {
	for range act.DayTimes(i) {
		return true
	}
	return false
}

func hasReservationLinks(grp ottrecidx.ScheduleGroupRef) bool {
	for range grp.GetReservationLinks() {
		return true
	}
	return false
}

func hasFacilityErrors(fac ottrecidx.FacilityRef) bool {
	for range fac.GetErrors() {
		return true
	}
	return false
}

// rawScrapedHTML normalizes a scraped HTML fragment by round-tripping it
// through the lenient html5 parser so it can be embedded in a page.
func rawScrapedHTML(raw string) templ.Component {
	nodes, err := html.ParseFragment(strings.NewReader(raw), &html.Node{
		Type:     html.ElementNode,
		DataAtom: atom.Body,
		Data:     "body",
	})
	if err != nil {
		return templ.Raw(`<pre class="raw-html">` + html.EscapeString(raw) + `</pre>`)
	}
	var b strings.Builder
	b.WriteString(`<div class="raw-html">`)
	for _, node := range nodes {
		if err := html.Render(&b, node); err != nil {
			return templ.Raw(`<pre class="raw-html">` + html.EscapeString(raw) + `</pre>`)
		}
	}
	b.WriteString(`</div>`)
	return templ.Raw(b.String())
}
