package templates

import (
	"fmt"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/pgaskin/ottrec-website/pkg/ottrecidx"
	"github.com/pgaskin/ottrec/schema"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

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

func hasReservationLinks(grp ottrecidx.ScheduleGroupRef) bool {
	for range grp.GetReservationLinks() {
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
