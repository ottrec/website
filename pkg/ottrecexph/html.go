// Package ottrecexph exports a XHTML-compatible version of the scraped data
// (either raw, parsed, or combined) primarily intended for manual comparisons,
// LLM consumption (though I'll probably make a proper MCP server since LLMs
// tend to get overwhelmed and miss times randomly), and debugging. It can also
// be used as a simplified target for re-scraping. It is intended to be the most
// similar in format to the original webpage. The structure may change without
// notice.
package ottrecexph

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pgaskin/ottrec-website/pkg/ottrecidx"
	"github.com/pgaskin/ottrec/schema"
	"github.com/pgaskin/xmlwriter"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type Options struct {
	// Show raw data only.
	Raw bool
	// Indentation to use, if any.
	Indent string
	// Canonical URL for the page, if known.
	Canonical string
	// Source protobuf URL, if known.
	Source string
	// Include JavaScript (e.g. to dim past/future schedules).
	Script bool
	// Extra stuff to add to the page.
	IncludeHead, IncludeTop, IncludeBottom func(w *xmlwriter.XMLWriter) error
}

const xhtml xmlwriter.NS = "http://www.w3.org/1999/xhtml"

const css = `
	html {
		color-scheme: light dark;
		background: light-dark(#eaecf5, #0b0d10);
		color: light-dark(#18181c, #d8dce8);
		font-size: 16px;
	}
	body {
		margin: 0;
		font-size: .875rem;
		line-height: 1.5;
		font-family: sans-serif;
	}
	h1, h2, h3, h4 {
		margin: 0;
		line-height: 1.3;
		font-family: sans-serif;
		font-weight: 700;
	}

	main > article {
		margin: 1rem;
		padding: .875rem 1.125rem 1.25rem;
		border-radius: .6rem;
		background: light-dark(#fff, #161a22);
		border: 1px solid light-dark(#dce2ee, #25303a);
		box-shadow: 0 1px 4px light-dark(rgba(0,0,0,.06), rgba(0,0,0,.4));
	}
	article > hgroup {
		padding-bottom: .55rem;
		margin-bottom: .75rem;
		border-bottom: 1px solid light-dark(#dce2ee, #25303a);
	}
	article > hgroup h1 {
		font-size: 1.1em;
	}
	article > hgroup p {
		margin: .2em 0 0;
		font-size: .8em;
		color: light-dark(#5c6880, #7888a4);
	}
	article > section {
		margin-top: .75rem;
	}
	article > section > h2 {
		margin-bottom: .35rem;
		font-size: .72em;
		font-weight: 700;
		letter-spacing: .06em;
		text-transform: uppercase;
		color: light-dark(#5c6880, #6878a0);
	}

	.facility-address-section, .schedule-group-reservation {
		background: light-dark(#f2f5fa, #1a1e2a);
		padding: .45em .7em;
		border-radius: .4rem;
		border: 1px solid light-dark(#dce2ee, #25303a);
	}
	.facility-special-hours, .facility-notifications, .schedule-group-schedule-changes {
		background: light-dark(#fffbeb, #1c1800);
		padding: .45em .7em;
		border-radius: .4rem;
		border: 1px solid light-dark(#fde68a, #3d3000);
	}
	.facility-special-hours .raw-html,
	.facility-notifications .raw-html,
	.schedule-group-schedule-changes .raw-html {
		background: transparent;
		border-color: transparent;
		padding-left: 0;
		padding-right: 0;
		margin-top: .2em;
	}
	.facility-errors {
		background: light-dark(#fff2f2, #1c0808);
		padding: .45em .7em;
		border-radius: .4rem;
		border: 1px solid light-dark(#fca5a5, #3d1010);
	}

	a {
		color: light-dark(#2563eb, #60a5fa);
		text-decoration: none;
	}
	a:hover, a:focus {
		text-decoration: underline;
	}

	address {
		font-style: normal;
		font-size: .85em;
	}
	address p {
		margin: .2em 0;
	}
	.facility-url a {
		font-size: .85em;
		word-break: break-all;
		color: light-dark(#5070a8, #7090c0);
	}

	.facility-errors ul {
		margin: .2em 0 0;
		padding-left: 1em;
		color: light-dark(#b91c1c, #f87171);
	}
	.facility-errors ul li {
		margin: .05em 0;
	}
	.facility-errors ul li::marker {
		content: "•\00a0";
	}

	.raw-html {
		font-size: .85em;
		background: light-dark(#f5f7fc, #161a22);
		padding: .4em .65em;
		border-radius: .35rem;
		margin-top: .35em;
		border: 1px solid light-dark(#dce2ee, #25303a);
	}
	.raw-html ul, .raw-html ol {
		margin: .15em 0;
		padding-left: 1.2em;
	}
	.raw-html li {
		margin: 0;
	}
	.raw-html li::marker {
		content: "•\00a0";
	}
	.raw-html p {
		margin: .2em 0;
	}
	.raw-html h3, .raw-html h4 {
		font-family: sans-serif;
		font-size: .85em;
		font-weight: 600;
		margin: .5em 0 .1em;
	}

	.facility-schedules > h2 {
		display: none;
	}

	section.schedule-group {
		margin-top: .5em;
	}
	section.schedule-group + section.schedule-group {
		margin-top: 1.75em;
	}
	section.schedule-group > h3 {
		margin-bottom: .45rem;
		font-size: .95em;
		font-weight: bold;
		color: light-dark(#18181c, #c0c8d8);
		border-bottom: 2px solid light-dark(#dce2ee, #25303a);
		padding-bottom: .3em;
	}
	section.schedule-group > section > h4 {
		font-size: .72em;
		font-weight: 700;
		letter-spacing: .06em;
		text-transform: uppercase;
		color: light-dark(#5c6880, #6878a0);
		margin-bottom: .25em;
	}
	section.schedule-group > section {
		margin-top: .6em;
	}
	section.schedule-group > section > ul {
		margin: .1em 0 0;
		padding-left: 0;
		font-size: .85em;
		list-style: none;
	}
	section.schedule-group > section > ul li {
		margin: 0;
	}

	.table-wrap {
		overflow-x: auto;
		margin-top: .65em;
	}

	table {
		table-layout: fixed;
		border-collapse: collapse;
		min-width: 100%;
		font-size: .8em;
	}
	table caption {
		text-align: left;
		font-weight: bold;
		padding: .25em .1em .4em;
		font-size: 1.1em;
		color: light-dark(#18181c, #c0c8d8);
	}
	table th, table td {
		vertical-align: top;
		border: 1px solid light-dark(#dce2ee, #25303a);
		padding: .25em .4em;
	}
	table th {
		font-weight: bold;
	}
	table thead th {
		background: light-dark(#edf1f8, #1a1e2a);
		font-size: .9em;
		text-align: center;
	}
	table tbody th {
		text-align: left;
		vertical-align: middle;
		width: 9rem;
		min-width: 7rem;
		max-width: 9rem;
		word-break: break-word;
		background: light-dark(#f7fafd, #181e28);
	}
	table td {
		text-align: center;
	}
	table td > span {
		display: block;
		white-space: nowrap;
	}

	footer {
		margin: 1rem;
		text-align: center;
		font-size: .75em;
		color: light-dark(#5c6880, #7888a4);
	}
	footer p {
		margin: .15em 0;
	}

	@media not print {
		table.schedule-past, table.schedule-future { opacity: .35; }
		table.schedule-past caption::after { content: " (past)"; font-weight: normal; }
		table.schedule-future caption::after { content: " (future)"; font-weight: normal; }
	}

	.activity-reservation-required,
	.activity-reservation-required-maybe {
		display: inline-block;
		border-radius: 3px;
		width: 14px;
		text-align: center;
		overflow: hidden;
		vertical-align: middle;
		padding: 1px;
		line-height: 1;
		font-size: 0;
		white-space: nowrap;
	}
	.activity-reservation-required::after,
	.activity-reservation-required-maybe::after {
		font-size: 9px;
		font-weight: 700;
	}
	.activity-reservation-required {
		background: light-dark(#dbeafe, #1a3060);
		color: light-dark(#1d4ed8, #60a5fa);
	}
	.activity-reservation-required::after {
		content: 'R';
	}
	.activity-reservation-required-maybe {
		background: light-dark(#dce2ee, #25303a);
		color: light-dark(#5c6880, #7888a4);
	}
	.activity-reservation-required-maybe::after {
		content: 'R?';
	}
`

const js = `
(function(){
	var today = new Date(); today.setHours(0,0,0,0);
	var t = today.toISOString().slice(0,10);
	document.querySelectorAll('table.schedule').forEach(function(tbl){
		var cap = tbl.querySelector('caption');
		if (!cap) return;
		var fe = cap.querySelector('time.schedule-date-from');
		var te = cap.querySelector('time.schedule-date-to');
		var f = fe ? fe.getAttribute('datetime') : null;
		var e = te ? te.getAttribute('datetime') : null;
		if (!f && !e) return;
		if (f && t < f) tbl.classList.add('schedule-future');
		else if (e && t > e) tbl.classList.add('schedule-past');
	});
})();
`

func HTML(data ottrecidx.DataRef, opt *Options) ([]byte, error) {
	if opt == nil {
		opt = new(Options)
	}

	b := bytes.NewBuffer(nil)
	x := xmlwriter.New(b)

	x.Indent(opt.Indent)
	x.Raw([]byte("<!DOCTYPE html>\n"))
	x.Start(xhtml, "html", xhtml.Bind(""))
	x.Attr(nil, "lang", "en")

	x.Start(nil, "head")
	{
		x.Start(nil, "meta")
		x.Attr(nil, "charset", "utf-8")
		x.End(true)

		x.Start(nil, "title")
		x.Text(false, "Ottawa Recreation Schedules")
		x.End(false)

		x.Start(nil, "meta")
		x.Attr(nil, "name", "generator")
		x.Attr(nil, "content", "ottrec")
		x.End(true)

		x.Start(nil, "meta")
		x.Attr(nil, "name", "robots")
		x.Attr(nil, "content", "noindex, nofollow")
		x.End(true)

		x.Start(nil, "meta")
		x.Attr(nil, "name", "dcterms.title")
		x.Attr(nil, "content", "Ottawa Recreation Schedules")
		x.End(true)

		x.Start(nil, "meta")
		x.Attr(nil, "name", "dcterms.format")
		if opt.Raw {
			x.Attr(nil, "content", "html-raw")
		} else {
			x.Attr(nil, "content", "html-parsed")
		}
		x.End(true)

		if u := opt.Canonical; u != "" {
			x.Start(nil, "meta")
			x.Attr(nil, "name", "dcterms.identifier")
			x.Attr(nil, "content", u)
			x.End(true)
		}

		if u := opt.Source; u != "" {
			x.Start(nil, "meta")
			x.Attr(nil, "name", "dcterms.source")
			x.Attr(nil, "content", u)
			x.End(true)
		}

		if t := data.Index().Updated(); !t.IsZero() {
			x.Start(nil, "meta")
			x.Attr(nil, "name", "dcterms.date")
			x.Attr(nil, "content", t.UTC().Format(time.RFC3339))
			x.End(true)
		}

		for s := range data.GetAttribution() {
			x.Start(nil, "meta")
			x.Attr(nil, "name", "dcterms.rights")
			x.Attr(nil, "content", s)
			x.End(true)
		}

		if u := opt.Canonical; u != "" {
			x.Start(nil, "link")
			x.Attr(nil, "rel", "canonical")
			x.Attr(nil, "href", u)
			x.End(true)
		}

		if inc := opt.IncludeHead; inc != nil {
			if err := inc(x); err != nil {
				return nil, err
			}
		}

		x.Start(nil, "style")
		x.Raw([]byte(`/*<![CDATA[*/`))
		for line := range strings.Lines(strings.TrimSpace(css)) {
			x.Raw([]byte(strings.TrimSpace(line)))
		}
		x.Raw([]byte(`/*]]>*/`))
		x.End(false)
	}
	x.End(false)

	x.Start(nil, "body")
	{
		if inc := opt.IncludeTop; inc != nil {
			if err := inc(x); err != nil {
				return nil, err
			}
		}

		x.Start(nil, "main")
		for fac := range data.Facilities() {
			x.Start(nil, "article")
			x.Attr(nil, "class", "facility")

			x.Start(nil, "hgroup")
			{
				x.Start(nil, "h1")
				x.Attr(nil, "class", "facility-name")
				x.Text(false, fac.GetName())
				x.End(false)

				if t := fac.GetSourceDate(); !t.IsZero() {
					x.Start(nil, "p")
					x.Text(true, "Updated ")
					x.Start(nil, "time")
					x.Attr(nil, "class", "facility-updated")
					x.Attr(nil, "datetime", t.Format(time.RFC3339))
					x.Text(false, t.Format("Mon Jan 2 15:04:05 2006"))
					x.End(false)
					x.End(false)
				}
			}
			x.End(false)

			x.Start(nil, "section")
			x.Attr(nil, "class", "facility-address-section")
			x.Start(nil, "h2")
			x.Text(false, "Address")
			x.End(false)
			x.Start(nil, "address")
			{
				if s := fac.GetAddress(); s != "" {
					x.Start(nil, "p")
					x.Attr(nil, "class", "facility-address")
					if lng, lat, ok := fac.GetLngLat(); ok {
						x.Attr(nil, "data-lat", strconv.FormatFloat(float64(lat), 'f', -1, 32))
						x.Attr(nil, "data-lng", strconv.FormatFloat(float64(lng), 'f', -1, 32))
					}
					for line := range strings.Lines(strings.TrimSpace(s)) {
						x.Text(true, strings.TrimSpace(line))
						x.Start(nil, "br")
						x.End(true)
					}
					x.End(false)
				}

				x.Start(nil, "p")
				x.Attr(nil, "class", "facility-url")
				x.Start(nil, "a")
				x.Attr(nil, "href", fac.GetSourceURL())
				x.Text(false, fac.GetSourceURL())
				x.End(false)
				x.End(false)
			}
			x.End(false)
			x.End(false)

			if h := strings.TrimSpace(fac.GetSpecialHoursHTML()); h != "" {
				x.Start(nil, "section")
				x.Attr(nil, "class", "facility-special-hours")
				x.Start(nil, "h2")
				x.Text(false, "Special Hours")
				x.End(false)
				rawHTML(x, h)
				x.End(false)
			}

			if h := strings.TrimSpace(fac.GetNotificationsHTML()); h != "" {
				x.Start(nil, "section")
				x.Attr(nil, "class", "facility-notifications")
				x.Start(nil, "h2")
				x.Text(false, "Notifications")
				x.End(false)
				rawHTML(x, h)
				x.End(false)
			}

			var hasErrors bool
			for e := range fac.GetErrors() {
				if !hasErrors {
					x.Start(nil, "section")
					x.Attr(nil, "class", "facility-errors")
					x.Start(nil, "h2")
					x.Text(false, "Errors")
					x.End(false)
					x.Start(nil, "ul")
					hasErrors = true
				}
				x.Start(nil, "li")
				x.Text(false, e)
				x.End(false)
			}
			if hasErrors {
				x.End(false)
				x.End(false)
			}

			x.Start(nil, "section")
			x.Attr(nil, "class", "facility-schedules")
			x.Start(nil, "h2")
			x.Text(false, "Schedules")
			x.End(false)
			for grp := range fac.ScheduleGroups() {
				x.Start(nil, "section")
				x.Attr(nil, "class", "schedule-group")

				x.Start(nil, "h3")
				if opt.Raw {
					x.Text(false, grp.GetLabel())
				} else {
					x.Attr(nil, "data-raw", grp.GetLabel())
					x.Text(false, grp.GetTitle())
				}
				x.End(false)

				if h := strings.TrimSpace(grp.GetScheduleChangesHTML()); h != "" {
					x.Start(nil, "section")
					x.Attr(nil, "class", "schedule-group-schedule-changes")
					x.Start(nil, "h4")
					x.Text(false, "Schedule Changes")
					x.End(false)
					rawHTML(x, h)
					x.End(false)
				}

				for range grp.GetReservationLinks() {
					x.Start(nil, "section")
					x.Attr(nil, "class", "schedule-group-reservation")
					x.Start(nil, "h4")
					x.Text(false, "Reservation")
					x.End(false)
					x.Start(nil, "ul")
					for u := range grp.GetReservationLinks() {
						x.Start(nil, "li")
						x.Start(nil, "a")
						x.Attr(nil, "class", "schedule-group-resvlink")
						x.Attr(nil, "href", u.URL)
						x.Text(false, u.Label)
						x.End(false)
						x.End(false)
					}
					x.End(false)
					x.End(false)
					break
				}

				if opt.Raw && grp.GetNoResv() {
					x.Start(nil, "p")
					x.Attr(nil, "class", "schedule-group-noresv")
					x.Text(false, "Reservation not required.")
					x.End(false)
				}

				for sch := range grp.Schedules() {
					x.Start(nil, "div")
					x.Attr(nil, "class", "table-wrap")
					x.Start(nil, "table")
					x.Attr(nil, "class", "schedule")

					x.Start(nil, "caption")
					if !opt.Raw {
						x.Attr(nil, "data-raw", sch.GetCaption())
					}
					if s := sch.GetName(); opt.Raw || s == "" {
						x.Text(false, sch.GetCaption())
					} else {
						x.Start(nil, "span")
						x.Attr(nil, "class", "schedule-name")
						x.Text(false, s)
						x.End(false)
						if er, ok := sch.ComputeEffectiveDateRange(); ok {
							if hasFrom, hasTo := !er.From.IsZero(), !er.To.IsZero(); hasFrom || hasTo {
								x.Text(false, " - ")
								x.Start(nil, "span")
								x.Attr(nil, "class", "schedule-date")
								if er.From != er.To {
									switch {
									case hasFrom && !hasTo:
										x.Text(false, "starting ")
									case !hasFrom && hasTo:
										x.Text(false, "until ")
									}
									if hasFrom {
										if er.From.IsValid() {
											x.Start(nil, "time")
											x.Attr(nil, "class", "schedule-date-from")
											if t, ok := er.From.GoTime(ottrecidx.TZ); ok {
												x.Attr(nil, "datetime", t.Format("2006-01-02"))
											}
											x.Text(false, er.From.String())
											x.End(false)
										} else {
											x.Text(false, "<invalid>")
										}
									}
									if hasFrom && hasTo {
										x.Text(false, " to ")
									}
								}
								if hasTo {
									if er.To.IsValid() {
										x.Start(nil, "time")
										x.Attr(nil, "class", "schedule-date-to")
										if t, ok := er.To.GoTime(ottrecidx.TZ); ok {
											x.Attr(nil, "datetime", t.Format("2006-01-02"))
										}
										x.Text(false, er.To.String())
										x.End(false)
									} else {
										x.Text(false, "<invalid>")
									}
								}
								x.End(false)
							}
						} else if s := sch.GetDate(); s != "" {
							x.Text(false, " - ")
							x.Start(nil, "span")
							x.Attr(nil, "class", "schedule-date")
							x.Text(false, s)
							x.End(false)
						}
					}
					x.End(false)

					x.Start(nil, "thead")
					x.Start(nil, "tr")
					x.Start(nil, "th")
					x.End(false)
					for i := range sch.NumDays() {
						x.Start(nil, "th")
						if !opt.Raw {
							x.Attr(nil, "data-raw", sch.GetDay(i))
						}
						if d, ok := sch.GetDayDate(i); opt.Raw || !ok {
							x.Text(false, sch.GetDay(i))
						} else {
							x.Text(false, d.String())
						}
						x.End(false)
					}
					x.End(false)
					x.End(false)

					x.Start(nil, "tbody")
					for act := range sch.Activities() {
						x.Start(nil, "tr")
						x.Start(nil, "th")
						if !opt.Raw {
							x.Attr(nil, "data-raw", act.GetLabel())
						}
						x.Start(nil, "span")
						x.Attr(nil, "class", "activity")
						if s := act.GetName(); opt.Raw || s == "" {
							x.Text(false, act.GetLabel())
						} else {
							x.Text(false, s)
						}
						x.End(false)
						if !opt.Raw {
							if req, def := act.GuessReservationRequirement(); req {
								x.Start(nil, "span")
								x.Attr(nil, "class", "activity-reservation")
								if def {
									x.Start(nil, "span")
									x.Attr(nil, "class", "activity-reservation-required")
									x.Attr(nil, "title", "reservation required")
									x.Text(false, " (reservation required)")
									x.End(false)
								} else {
									x.Start(nil, "span")
									x.Attr(nil, "class", "activity-reservation-required-maybe")
									x.Attr(nil, "title", "reservation may be required")
									x.Text(false, " (reservation may be required)")
									x.End(false)
								}
								x.End(false)
							}
						}
						x.End(false)
						for i := range sch.NumDays() {
							x.Start(nil, "td")
							for tm := range act.DayTimes(i) {
								x.Start(nil, "span")
								if !opt.Raw {
									x.Attr(nil, "data-raw", tm.GetLabel())
								}
								if r, ok := tm.GetRange(); opt.Raw || !ok {
									x.Text(false, tm.GetLabel())
								} else {
									clockRange(x, r)
								}
								x.End(false)
							}
							x.End(false)
						}
						x.End(false)
					}
					x.End(false)

					x.End(false)
					x.End(false)
				}

				x.End(false)
			}
			x.End(false)

			x.End(false)
		}
		x.End(false)

		{
			started := false
			for s := range data.GetAttribution() {
				if !started {
					x.Start(nil, "footer")
					started = true
				}
				x.Start(nil, "p")
				x.Text(false, s)
				x.End(false)
			}
			if started {
				x.End(false)
			}
		}

		if opt.Script {
			x.Start(nil, "script")
			x.Raw([]byte("//<![CDATA[\n" + strings.TrimSpace(js) + "\n//]]>"))
			x.End(false)
		}

		if inc := opt.IncludeBottom; inc != nil {
			if err := inc(x); err != nil {
				return nil, err
			}
		}
	}
	x.End(false)

	x.End(false)

	return b.Bytes(), x.Close()
}

func clockRange(x *xmlwriter.XMLWriter, r schema.ClockRange) {
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
	et := fmt.Sprintf("%d:%02d%s", edh, em, esuf)

	x.Start(nil, "time")
	x.Attr(nil, "datetime", fmt.Sprintf("%02d:%02d", sh, sm))
	x.Text(false, st)
	x.End(false)
	x.Text(false, "\u2013")
	x.Start(nil, "time")
	x.Attr(nil, "datetime", fmt.Sprintf("%02d:%02d", eh, em))
	x.Text(false, et)
	x.End(false)
}

func rawHTML(x *xmlwriter.XMLWriter, raw string) {
	nodes, err := html.ParseFragment(strings.NewReader(raw), &html.Node{
		Type:     html.ElementNode,
		DataAtom: atom.Body,
		Data:     "body",
	})
	if err == nil {
		b := bytes.NewBuffer(nil)
		x2 := xmlwriter.New(b)
		x2.Start(xhtml, "div", xhtml.Bind(""))
		x2.Attr(nil, "class", "raw-html")
		for _, node := range nodes {
			renderHTML(x2, node)
		}
		x2.End(false)
		err = x2.Close()
		if err == nil {
			x.Raw(b.Bytes())
			return
		} else {
			err = fmt.Errorf("render: %w", err)
		}
	} else {
		err = fmt.Errorf("parse: %w", err)
	}
	x.Start(nil, "pre")
	x.Attr(nil, "class", "raw-html")
	x.Attr(nil, "data-error", "convert raw html: "+err.Error())
	x.Text(false, raw)
	x.End(false)
}

func renderHTML(w *xmlwriter.XMLWriter, n *html.Node) {
	switch n.Type {
	case html.ErrorNode:
		// ignore
	case html.TextNode:
		w.Text(false, n.Data)
	case html.DocumentNode:
		for c := range n.ChildNodes() {
			renderHTML(w, c)
		}
		return
	case html.ElementNode:
		w.Start(nil, n.Data)
		for _, a := range n.Attr {
			// note: I don't bother with namespace handling here
			w.Attr(nil, a.Key, a.Val)
		}
		for c := range n.ChildNodes() {
			renderHTML(w, c)
		}
		// note: will emit an error if there were children
		w.End(map[string]bool{
			"area":   true,
			"base":   true,
			"br":     true,
			"col":    true,
			"embed":  true,
			"hr":     true,
			"img":    true,
			"input":  true,
			"keygen": true,
			"link":   true,
			"meta":   true,
			"param":  true,
			"source": true,
			"track":  true,
			"wbr":    true,
		}[n.Data]) // will emit an error if has children
	case html.CommentNode:
		w.Comment(true, n.Data)
	case html.DoctypeNode:
		// ignore
	case html.RawNode:
		// ignore
		// w.Raw([]byte(n.Data))
	default:
		// ignore
	}
}
