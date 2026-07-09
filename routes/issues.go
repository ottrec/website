package routes

// this file renders the /api/data/issues report: a self-contained HTML page
// listing everything in the current dataset that may warrant manual review
// (scrape errors, parse ambiguities, data anomalies, enrichment issues, and
// the freeform HTML blocks), grouped by facility.

import (
	"fmt"
	"html"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/ottrec/data-enrichment/enrich"
	epb "github.com/ottrec/data-enrichment/schema"
	"github.com/ottrec/scraper/schema"
	"github.com/ottrec/website/internal/httpx"
	"github.com/ottrec/website/pkg/ottrecidx"
	"github.com/ottrec/website/pkg/ottregions"
)

// TODO: rewrite this (I mostly vibe-coded it with vague instructions unlike the
// other site pages, so it's kinda garbage, but it's better than having to read
// everything), maybe put in misc instead of website

// websiteDataIssuesHandler serves the dataset issues report. Unlinked and
// noindex, for internal use; it reruns the enrichment on a cache miss, so
// it's not cheap.
type websiteDataIssuesHandler struct {
	websiteHandlerBase
}

func (h *websiteDataIssuesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex")

	var (
		data ottrecidx.DataRef
		ok   bool
	)
	if h.Data != nil {
		data, ok = h.Data()
	}
	if !ok {
		http.Error(w, "data not available, try again later", http.StatusServiceUnavailable)
		return
	}

	etag := httpx.NewETag().
		MixExe().
		Mix(data.Index().Hash()).
		ETag().
		Weaken() // weak: built from the data hash, not the response bytes
	w.Header().Set("Cache-Control", "public, no-cache")
	if etag.Handled(w, r) {
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(BuildIssuesReport(data))
}

// issueEsc is a short alias since almost everything here needs escaping.
func issueEsc(s string) string { return html.EscapeString(s) }

// issueCode wraps a raw value in an escaped <code> element.
func issueCode(s string) string { return "<code>" + issueEsc(s) + "</code>" }

// issueCtx renders the non-empty parts as a muted breadcrumb prefix locating
// an item within the facility (group label, schedule caption, day, ...).
func issueCtx(parts ...string) string {
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString(" &rsaquo; ")
		}
		b.WriteString(issueEsc(p))
	}
	if b.Len() == 0 {
		return ""
	}
	return `<span class="ctx">` + b.String() + `</span> `
}

// issueDay strips the weekday digit so dates compare at day granularity.
func issueDay(d schema.Date) int32 { return int32(d) / 10 }

// issueDur formats a duration in minutes as 1h30m.
func issueDur(min schema.ClockTime) string {
	h, m := min/60, min%60
	switch {
	case h == 0:
		return fmt.Sprintf("%dm", m)
	case m == 0:
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh%dm", h, m)
}

// issueRangesOverlap reports whether two effective schedule date ranges can
// apply to the same date. Missing ranges and open sides overlap everything
// (schedule ranges are negative-only: they can rule dates out, not in).
func issueRangesOverlap(a schema.DateRange, aok bool, b schema.DateRange, bok bool) bool {
	if !aok || !bok {
		return true
	}
	if !a.To.IsZero() && !b.From.IsZero() && issueDay(a.To) < issueDay(b.From) {
		return false
	}
	if !b.To.IsZero() && !a.From.IsZero() && issueDay(b.To) < issueDay(a.From) {
		return false
	}
	return true
}

// BuildIssuesReport renders the report page for the dataset.
func BuildIssuesReport(data ottrecidx.DataRef) []byte {
	out := enrich.EnrichVersion("", data)

	objsByFac := map[string][]*epb.Object{}
	for _, o := range out.GetObjects() {
		objsByFac[o.GetFacility()] = append(objsByFac[o.GetFacility()], o)
	}
	placements := issuesPlacementIndex(out)

	counts := map[string]int{}

	updatedT := data.Index().Updated()
	var updatedDay int32
	if !updatedT.IsZero() {
		updatedDay = issueDay(schema.MakeDateFromGo(updatedT.In(ottrecidx.TZ)))
	}

	type facSection struct {
		items int
		html  string
	}
	var secs []facSection
	facNames := map[string]int{}

	// kindIDs maps item kinds to the short tokens used to join items to the
	// filter checkboxes
	kindIDs := map[string]string{}
	kindID := func(kind string) string {
		id, ok := kindIDs[kind]
		if !ok {
			id = fmt.Sprintf("k%d", len(kindIDs))
			kindIDs[kind] = id
		}
		return id
	}

	var allNames []string
	for fac := range data.Facilities() {
		allNames = append(allNames, fac.GetName())
	}

	fi := 0
	for fac := range data.Facilities() {
		fi++
		name := fac.GetName()
		facNames[name]++

		var errs, amb, anom, cards, blocks []string
		add := func(list *[]string, kind, item string) {
			*list = append(*list, `<li data-kind="`+kindID(kind)+`">`+item+`</li>`)
			counts[kind]++
		}

		// scrape errors
		for e := range fac.GetErrors() {
			add(&errs, "scrape: error", issueEsc(e))
		}

		// facility-level ambiguities
		if _, _, ok := fac.GetLngLat(); !ok {
			add(&amb, "scrape: no coordinates", "no parsed coordinates")
		} else if fac.Region() == ottregions.RegionUnknown || fac.Sector() == ottregions.SectorUnknown {
			add(&anom, "anomaly: coordinates outside ottawa", "coordinates fall outside the known Ottawa regions")
		}
		if fac.GetAddress() == "" {
			add(&amb, "scrape: no address", "no address")
		}
		if fac.GetSourceURL() == "" {
			add(&amb, "scrape: no source url", "no source URL")
		}

		seenActName := map[string]bool{} // dedupe unparsed activity names per facility

		for grp := range fac.ScheduleGroups() {
			glabel := grp.GetLabel()

			if grp.GetTitle() == "" && glabel != "" {
				add(&amb, "scrape: group title unparsed", issueCtx(glabel)+"group title not parsed from label")
			}

			// indefinite reservation requirement guesses, aggregated per group
			var indef [2][]string // by guess: [0] not required, [1] required
			for act := range grp.Activities() {
				if req, definite := act.GuessReservationRequirement(); !definite {
					i := 0
					if req {
						i = 1
					}
					if !slices.Contains(indef[i], act.GetLabel()) {
						indef[i] = append(indef[i], act.GetLabel())
					}
				}
			}
			for i, guess := range [2]string{"not required", "required"} {
				if len(indef[i]) > 0 {
					add(&amb, "scrape: reservation requirement indefinite",
						issueCtx(glabel)+fmt.Sprintf("indefinite reservation requirement (guess: %s) for %d activities: %s",
							guess, len(indef[i]), issueEsc(strings.Join(indef[i], "; "))))
				}
			}

			// per-schedule state for the group-wide overlap checks
			type schedEnt struct {
				caption string
				er      schema.DateRange
				erOK    bool
				holiday bool
			}
			var schedEnts []schedEnt
			type sessEnt struct {
				sched                    int // index into schedEnts
				day, actLabel, timeLabel string
				date                     schema.Date  // single day date, 0 if none
				wd                       time.Weekday // -1 if unknown
				rng                      schema.ClockRange
			}
			sessions := map[string][]sessEnt{} // by normalized activity name

			nsched := 0
			for sched := range grp.Schedules() {
				nsched++
				caption := sched.GetCaption()
				er, erOK := sched.ComputeEffectiveDateRange()
				schedEnts = append(schedEnts, schedEnt{caption, er, erOK, sched.LikelyHolidaySchedule()})
				si := len(schedEnts) - 1

				if sched.GetName() == "" && caption != "" {
					add(&amb, "scrape: schedule name unparsed", issueCtx(glabel)+"schedule name not parsed from caption "+issueCode(caption))
				}
				if r, ok := sched.GetDateRange(); !ok && sched.GetDate() != "" {
					add(&amb, "scrape: date range unparsed", issueCtx(glabel, caption)+"date range not parsed from "+issueCode(sched.GetDate()))
				} else if ok && !erOK {
					add(&amb, "scrape: effective range uncomputable", issueCtx(glabel, caption)+"effective date range not computable from "+issueCode(r.String()))
				}
				if erOK && !er.To.IsZero() && updatedDay != 0 && issueDay(er.To) < updatedDay {
					add(&anom, "anomaly: schedule ended before dataset update", issueCtx(glabel, caption)+"ended "+issueCode(er.To.String())+", before the dataset update")
				}
				if erOK {
					from, to, _ := er.GoTime(ottrecidx.TZ)
					if !from.IsZero() && !to.IsZero() && to.Sub(from) > 370*24*time.Hour {
						add(&anom, "anomaly: implausibly long date range", issueCtx(glabel, caption)+"effective range "+issueCode(er.String())+" spans more than a year")
					}
					if !from.IsZero() && !updatedT.IsZero() && from.Sub(updatedT) > 183*24*time.Hour {
						add(&anom, "anomaly: date range far in future", issueCtx(glabel, caption)+"effective range "+issueCode(er.String())+" starts more than 6 months after the dataset update")
					}
				}
				if lc := strings.ToLower(caption); caption != "" {
					lname := strings.ToLower(name)
					for _, other := range allNames {
						lo := strings.ToLower(other)
						if other != name && !strings.Contains(lname, lo) && strings.Contains(lc, lo) {
							add(&anom, "anomaly: caption names another facility", issueCtx(glabel)+"caption "+issueCode(caption)+" names another facility ("+issueEsc(other)+")")
						}
					}
				}

				seenDay := map[string]int{}
				for di := range sched.NumDays() {
					day := sched.GetDay(di)
					if day == "" {
						continue
					}
					if seenDay[day]++; seenDay[day] == 2 {
						add(&anom, "anomaly: duplicate day label", issueCtx(glabel, caption)+"duplicate day label "+issueCode(day))
					}
					if _, ok := sched.GetDayDate(di); !ok {
						add(&amb, "scrape: day label unparsed", issueCtx(glabel, caption)+"day label not parsed: "+issueCode(day))
					}
					if sd, ok := sched.SingleDayDate(di); ok {
						if erOK && ((!er.From.IsZero() && issueDay(sd) < issueDay(er.From)) || (!er.To.IsZero() && issueDay(sd) > issueDay(er.To))) {
							add(&anom, "anomaly: day date outside date range", issueCtx(glabel, caption)+"day "+issueCode(day)+" ("+issueEsc(sd.String())+") falls outside the effective range "+issueCode(er.String()))
						}
						if d, ok := sched.GetDayDate(di); ok {
							if lw, ok := d.Weekday(); ok {
								if rw, ok := sd.Weekday(); ok && rw != lw {
									add(&anom, "anomaly: date/weekday contradiction", issueCtx(glabel, caption)+"day "+issueCode(day)+" says "+issueEsc(lw.String())+" but the date falls on a "+issueEsc(rw.String()))
								}
							}
						}
					}
				}

				nact := 0
				seenAct := map[string]int{}
				for act := range sched.Activities() {
					nact++
					alabel := act.GetLabel()
					if act.GetName() == "" && alabel != "" && !seenActName[alabel] {
						seenActName[alabel] = true
						add(&amb, "scrape: activity name unparsed", issueCtx(glabel, caption)+"activity name not normalized from label "+issueCode(alabel))
					}
					if seenAct[alabel]++; seenAct[alabel] == 2 {
						add(&anom, "anomaly: duplicate activity label", issueCtx(glabel, caption)+"duplicate activity label "+issueCode(alabel))
					}
					key := act.GetName()
					if key == "" {
						key = strings.ToLower(alabel)
					}

					type durEnt struct {
						day, timeLabel string
						dur            schema.ClockTime
					}
					var durs []durEnt

					ntimes := 0
					for t := range act.Times() {
						ntimes++
						di := t.GetScheduleDayIndex()
						day := sched.GetDay(di)
						tlabel := t.GetLabel()
						wd, wdOK := t.GetWeekday()
						rng, rngOK := t.GetRange()

						if !wdOK {
							add(&amb, "scrape: time weekday unparsed", issueCtx(glabel, caption, day, alabel)+"weekday not parsed from time label "+issueCode(tlabel))
						}
						if !rngOK {
							add(&amb, "scrape: time range unparsed", issueCtx(glabel, caption, day, alabel)+"clock range not parsed from time label "+issueCode(tlabel))
							continue
						}

						dayWd := time.Weekday(-1)
						if d, ok := sched.GetDayDate(di); ok {
							if w, ok := d.Weekday(); ok {
								dayWd = w
							}
						}
						if wdOK && dayWd >= 0 && wd != dayWd {
							add(&anom, "anomaly: weekday mismatch", issueCtx(glabel, caption, day, alabel)+"time label "+issueCode(tlabel)+" weekday ("+issueEsc(wd.String())+") does not match the day column")
						}

						switch dur := rng.End - rng.Start; {
						case dur > 18*60:
							add(&anom, "anomaly: session longer than 18h", issueCtx(glabel, caption, day, alabel)+"session "+issueCode(tlabel)+" is longer than 18h ("+issueEsc(rng.Format(true))+")")
						case dur <= 0:
							add(&anom, "anomaly: non-positive session length", issueCtx(glabel, caption, day, alabel)+"session "+issueCode(tlabel)+" has a non-positive length ("+issueEsc(rng.Format(true))+")")
						case dur < 15:
							add(&anom, "anomaly: session shorter than 15m", issueCtx(glabel, caption, day, alabel)+"session "+issueCode(tlabel)+" is shorter than 15m ("+issueEsc(rng.Format(true))+")")
						}
						if rng.End > 24*60 {
							add(&anom, "anomaly: overnight session", issueCtx(glabel, caption, day, alabel)+"session "+issueCode(tlabel)+" crosses midnight ("+issueEsc(rng.Format(true))+")")
						}
						if rng.Start%5 != 0 || rng.End%5 != 0 {
							add(&anom, "anomaly: time not aligned to 5 minutes", issueCtx(glabel, caption, day, alabel)+"session "+issueCode(tlabel)+" is not aligned to 5 minutes ("+issueEsc(rng.Format(true))+")")
						}
						if rng.Start < 5*60 || (rng.End > 23*60 && rng.End <= 24*60) {
							add(&anom, "anomaly: implausible session hours", issueCtx(glabel, caption, day, alabel)+"session "+issueCode(tlabel)+" starts before 5 am or ends after 11 pm ("+issueEsc(rng.Format(true))+")")
						}

						sdate, _ := sched.SingleDayDate(di)
						if sdate < 0 {
							sdate = 0
						}
						ewd := dayWd
						if ewd < 0 && wdOK {
							ewd = wd
						}
						sessions[key] = append(sessions[key], sessEnt{si, day, alabel, tlabel, sdate, ewd, rng})
						if dur := rng.End - rng.Start; dur > 0 {
							durs = append(durs, durEnt{day, tlabel, dur})
						}
					}
					if ntimes == 0 {
						add(&anom, "anomaly: activity with no times", issueCtx(glabel, caption)+"activity "+issueCode(alabel)+" has no times")
					}

					// a unique longest session at least twice as long as every
					// other session of the activity in this schedule often
					// means a typo or an am/pm mixup; a long duration that
					// repeats is taken as intentional
					if len(durs) >= 3 {
						var max1, max2 schema.ClockTime
						max1Count := 0
						for _, d := range durs {
							switch {
							case d.dur > max1:
								max2, max1, max1Count = max1, d.dur, 1
							case d.dur == max1:
								max1Count++
							case d.dur > max2:
								max2 = d.dur
							}
						}
						if max1Count == 1 && max1 >= 2*max2 && max1-max2 >= 60 {
							for _, d := range durs {
								if d.dur == max1 {
									add(&anom, "anomaly: session duration outlier", issueCtx(glabel, caption, d.day, alabel)+"session "+issueCode(d.timeLabel)+
										fmt.Sprintf(" is more than twice as long as the activity's other sessions (%s vs at most %s)", issueDur(d.dur), issueDur(max2)))
								}
							}
						}
					}
				}
				if nact == 0 {
					add(&anom, "anomaly: schedule with no activities", issueCtx(glabel, caption)+"schedule has no activities")
				}
			}
			if nsched == 0 {
				add(&anom, "anomaly: group with no schedules", issueCtx(glabel)+"group has no schedules")
			}

			// overlapping effective date ranges between schedules in the group
			schedDesc := func(i int) string {
				e := schedEnts[i]
				r := "no effective range"
				if e.erOK {
					r = e.er.String()
				}
				if e.holiday {
					r += ", likely holiday"
				}
				return issueCode(e.caption) + " (" + issueEsc(r) + ")"
			}
			for i := range schedEnts {
				for j := i + 1; j < len(schedEnts); j++ {
					if issueRangesOverlap(schedEnts[i].er, schedEnts[i].erOK, schedEnts[j].er, schedEnts[j].erOK) {
						add(&anom, "anomaly: overlapping schedule date ranges", issueCtx(glabel)+"overlapping date ranges: "+schedDesc(i)+" and "+schedDesc(j))
					}
				}
			}

			// overlapping sessions for the same activity and weekday/date,
			// across schedules whose date ranges can apply at the same time
			for _, key := range slices.Sorted(maps.Keys(sessions)) {
				ents := sessions[key]
				for i := range ents {
					for j := i + 1; j < len(ents); j++ {
						a, b := ents[i], ents[j]
						var when string
						switch {
						case a.date != 0 && b.date != 0:
							if issueDay(a.date) != issueDay(b.date) {
								continue
							}
							when = a.date.String()
						case a.wd >= 0 && b.wd >= 0:
							if a.wd != b.wd {
								continue
							}
							when = a.wd.String()
						default:
							continue
						}
						if !(a.rng.Start < b.rng.End && b.rng.Start < a.rng.End) {
							continue
						}
						if a.sched != b.sched && !issueRangesOverlap(schedEnts[a.sched].er, schedEnts[a.sched].erOK, schedEnts[b.sched].er, schedEnts[b.sched].erOK) {
							continue
						}
						side := func(e sessEnt) string {
							return issueCode(e.timeLabel) + " [" + issueEsc(e.actLabel) + ", " + issueEsc(schedEnts[e.sched].caption) + "]"
						}
						add(&anom, "anomaly: overlapping sessions", issueCtx(glabel)+"overlapping sessions for "+issueCode(key)+" on "+issueEsc(when)+": "+side(a)+" and "+side(b))
					}
				}
			}
		}

		// enrichment issues
		flaggedBlocks := map[string]bool{}
		for _, o := range objsByFac[name] {
			var flags []string
			effects, unknownEffect := issuesEffectsText(o.GetEffects())
			switch o.GetKind() {
			case epb.Object_NOTICE:
				if unknownEffect {
					flags = append(flags, "unknown-effect")
				}
				if len(o.GetEffects()) == 0 {
					flags = append(flags, "no-effects")
				}
				switch o.GetMatchQuality() {
				case epb.Object_MULTIPLE:
					flags = append(flags, "match/multiple")
				case epb.Object_NONE:
					flags = append(flags, "match/none")
				case epb.Object_FUZZY:
					flags = append(flags, "match/fuzzy")
				}
				for _, a := range o.GetAmbiguities() {
					flags = append(flags, "ambiguous/"+a)
				}
			case epb.Object_UNPARSED:
				flags = append(flags, "unparsed/"+o.GetReason())
			case epb.Object_IGNORED:
				// structure/boilerplate, not an issue
			default:
				flags = append(flags, "unknown-kind/"+o.GetKind().String())
			}
			if len(flags) == 0 {
				continue
			}
			var kinds []string
			for _, f := range flags {
				counts["enrich: "+f]++
				kinds = append(kinds, kindID("enrich: "+f))
			}
			flaggedBlocks[o.GetBlockHash()] = true
			cards = append(cards, issuesEnrichCard(o, flags, strings.Join(kinds, " "), effects, placements))
		}

		// freeform blocks, as context for the flagged objects above; blocks
		// the enrichment handled cleanly are omitted
		addBlock := func(source, group, blockHTML string) {
			if strings.TrimSpace(blockHTML) == "" || !flaggedBlocks[enrich.BlockHash(blockHTML)] {
				return
			}
			label := source
			if group != "" {
				label += " [" + group + "]"
			}
			blocks = append(blocks, fmt.Sprintf(`<div class="block" data-kind="%s"><div class="blockhead">%s (%d bytes)</div><pre>%s</pre></div>`,
				kindID("freeform: "+source), issueEsc(label), len(blockHTML), issueEsc(blockHTML)))
			counts["freeform: "+source]++
		}
		for grp := range fac.ScheduleGroups() {
			addBlock("schedule_changes", grp.GetLabel(), grp.GetScheduleChangesHTML())
		}
		addBlock("special_hours", "", fac.GetSpecialHoursHTML())
		addBlock("notifications", "", fac.GetNotificationsHTML())

		items := len(errs) + len(amb) + len(anom) + len(cards) + len(blocks)
		if items == 0 {
			continue
		}

		var b strings.Builder
		b.WriteString(`<section><h2>`)
		if u := fac.GetSourceURL(); u != "" {
			fmt.Fprintf(&b, `<a href="%s" target="_blank" rel="noreferrer noopener">%s</a>`, issueEsc(u), issueEsc(name))
		} else {
			b.WriteString(issueEsc(name))
		}
		unit := "items"
		if items == 1 {
			unit = "item"
		}
		fmt.Fprintf(&b, ` <span class="n">%d %s</span></h2>`, items, unit)

		var meta []string
		if a := fac.GetAddress(); a != "" {
			meta = append(meta, issueEsc(a))
		}
		if lng, lat, ok := fac.GetLngLat(); ok {
			meta = append(meta, fmt.Sprintf("lng/lat %.5f, %.5f", lng, lat))
		}
		if t := fac.GetSourceDate(); !t.IsZero() {
			meta = append(meta, "sourced "+t.In(ottrecidx.TZ).Format("2006-01-02"))
		}
		if len(meta) > 0 {
			fmt.Fprintf(&b, `<p class="meta">%s</p>`, strings.Join(meta, " &middot; "))
		}

		section := func(title string, items []string, list bool) {
			if len(items) == 0 {
				return
			}
			fmt.Fprintf(&b, `<div class="sub"><h3>%s <span class="n">%d</span></h3>`, title, len(items))
			if list {
				b.WriteString("<ul>")
			}
			for _, it := range items {
				b.WriteString(it)
			}
			if list {
				b.WriteString("</ul>")
			}
			b.WriteString("</div>")
		}
		section("scrape errors", errs, true)
		section("scrape ambiguities", amb, true)
		section("anomalies", anom, true)
		section("enrichment", cards, false)
		section("freeform blocks", blocks, false)
		b.WriteString("</section>")

		secs = append(secs, facSection{items, b.String()})
	}

	// dataset-level anomalies
	var dataAnom []string
	for _, name := range slices.Sorted(maps.Keys(facNames)) {
		if n := facNames[name]; n > 1 {
			dataAnom = append(dataAnom, fmt.Sprintf(`<li data-kind="%s">duplicate facility name %s (%d occurrences)</li>`,
				kindID("anomaly: duplicate facility name"), issueCode(name), n))
			counts["anomaly: duplicate facility name"]++
		}
	}

	total := 0
	for _, sec := range secs {
		total += sec.items
	}
	total += len(dataAnom)

	var b strings.Builder
	b.WriteString("<!doctype html>\n<meta charset=\"utf-8\">\n<title>data issues report</title>\n")
	b.WriteString(issuesPageCSS)
	b.WriteString(`<div class="meta"><h1>data issues report</h1><p>`)
	fmt.Fprintf(&b, `dataset %s`, issueCode(data.Index().Hash()))
	if t := data.Index().Updated(); !t.IsZero() {
		fmt.Fprintf(&b, ` &middot; updated %s`, issueEsc(t.In(ottrecidx.TZ).Format("2006-01-02 15:04:05 MST")))
	}
	fmt.Fprintf(&b, ` &middot; enrichment generated %s`, issueEsc(out.GetGenerated().AsTime().In(ottrecidx.TZ).Format("2006-01-02 15:04:05 MST")))
	fmt.Fprintf(&b, ` &middot; %d items across %d of %d facilities</p></div>`, total, len(secs), fi)

	// item kinds as filter checkboxes; unchecking hides that kind below (a
	// multi-kind item stays visible while any of its kinds is checked)
	b.WriteString(`<h3>item counts` +
		` <button type="button" class="fbtn" id="fall">all</button>` +
		` <button type="button" class="fbtn" id="fnone">none</button>` +
		` <button type="button" class="fbtn" id="freset">reset</button></h3>` +
		`<div class="filters">`)
	for _, k := range slices.Sorted(maps.Keys(counts)) {
		checked := ` checked`
		switch k {
		case "enrich: ambiguous/meridiem-inferred", "scrape: reservation requirement indefinite":
			checked = ""
		}
		fmt.Fprintf(&b, `<label><input type="checkbox"%s autocomplete="off" value="%s"> %s <span class="n">%d</span></label>`,
			checked, kindIDs[k], issueEsc(k), counts[k])
	}
	b.WriteString(`</div>`)

	b.WriteString(`<h3>enrichment stats</h3><table class="stats">`)
	for _, k := range slices.Sorted(maps.Keys(out.GetStats())) {
		fmt.Fprintf(&b, "<tr><td>%d</td><td>%s</td></tr>", out.GetStats()[k], issueEsc(k))
	}
	b.WriteString("</table>")

	if len(dataAnom) > 0 {
		b.WriteString(`<div class="sub"><h3>dataset anomalies</h3><ul>`)
		for _, it := range dataAnom {
			b.WriteString(it)
		}
		b.WriteString("</ul></div>")
	}

	for _, sec := range secs {
		b.WriteString(sec.html)
	}
	b.WriteString(issuesPageJS)
	return []byte(b.String())
}

// issuesEnrichCard renders one enrichment object flagged for review. kinds
// holds the space-separated filter tokens for the object's flags.
func issuesEnrichCard(o *epb.Object, flags []string, kinds, effects string, placements map[string][]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="card" data-kind="%s"><div class="cardhead">`, kinds)
	for _, f := range flags {
		cls := "warn"
		if strings.HasPrefix(f, "unparsed/") || strings.HasPrefix(f, "unknown-") {
			cls = "bad"
		}
		fmt.Fprintf(&b, `<span class="badge %s">%s</span> `, cls, issueEsc(f))
	}
	fmt.Fprintf(&b, `<code>%s</code> seq %d`, issueEsc(o.GetId()), o.GetSeq())
	b.WriteString(`</div>`)

	row := func(label, val string) {
		if val != "" {
			fmt.Fprintf(&b, `<div class="row"><span class="lbl">%s</span> %s</div>`, label, val)
		}
	}
	esc := issueEsc

	src := strings.ToLower(o.GetSource().String())
	if g := o.GetSourceGroup(); g != "" {
		src += " [" + g + "]"
	}
	row("source", esc(src))
	if s := o.GetSources(); len(s) > 1 {
		var all []string
		for _, x := range s {
			all = append(all, strings.ToLower(x.String()))
		}
		row("sources", esc(strings.Join(all, ", ")))
	}
	row("section", esc(o.GetSection()))
	if dt := o.GetDateText(); dt != "" && dt != o.GetRawText() {
		row("date text", esc(dt))
	}
	row("text", esc(o.GetRawText()))
	row("dates", esc(issuesDateSpanText(o.GetDates())))
	row("time", esc(issuesTimeAssocText(o.GetTime())))
	row("effects", esc(effects))
	if q := o.GetMatchQuality(); q != epb.Object_MATCH_QUALITY_UNSPECIFIED {
		row("match", esc(strings.ToLower(q.String())))
	}
	row("phrase", esc(o.GetPhrase()))
	row("amenity", esc(o.GetAmenity()))
	row("candidates", esc(strings.Join(o.GetCandidates(), "; ")))
	row("ambiguities", esc(strings.Join(o.GetAmbiguities(), ", ")))
	row("placed", esc(strings.Join(placements[o.GetId()], "; ")))
	if h := o.GetRawHtml(); h != "" {
		fmt.Fprintf(&b, `<div class="blockhead">raw html (%d bytes)</div><pre>%s</pre>`, len(h), esc(h))
	}
	b.WriteString(`</div>`)
	return b.String()
}

// issuesPlacementIndex maps object id to human-readable positions in the
// enrichment output tree.
func issuesPlacementIndex(out *epb.Output) map[string][]string {
	idx := map[string][]string{}
	add := func(ids []string, where string) {
		for _, id := range ids {
			idx[id] = append(idx[id], where)
		}
	}
	for _, f := range out.GetFacilities() {
		add(f.GetObjects(), "facility")
		for _, g := range f.GetGroups() {
			add(g.GetObjects(), "group "+g.GetLabel())
			for _, a := range g.GetActivities() {
				where := "activity " + a.GetLabel()
				if a.GetNovel() {
					where += " (novel)"
				}
				add(a.GetObjects(), where)
				for _, s := range a.GetSessions() {
					sess := fmt.Sprintf("%s %s-%s [%s]",
						schema.Date(s.GetDate()),
						schema.ClockTime(s.GetStart()).Format(true),
						schema.ClockTime(s.GetEnd()).Format(true),
						a.GetLabel())
					add(s.GetObjects(), "session "+sess)
					add(s.GetAdded(), "added session "+sess)
				}
			}
		}
	}
	return idx
}

func issuesDateSpanText(d *epb.DateSpan) string {
	if d == nil {
		return ""
	}
	var parts []string
	for _, x := range d.GetDates() {
		parts = append(parts, schema.Date(x).String())
	}
	if d.HasFrom() || d.HasTo() {
		parts = append(parts, schema.DateRange{From: schema.Date(d.GetFrom()), To: schema.Date(d.GetTo())}.String())
	}
	for _, x := range d.GetWeekdays() {
		parts = append(parts, schema.Date(x).String())
	}
	if d.GetOpenEnded() {
		parts = append(parts, "(open-ended)")
	}
	return strings.Join(parts, ", ")
}

func issuesTimeAssocText(t *epb.TimeAssoc) string {
	if t == nil {
		return ""
	}
	var parts []string
	if t.HasStart() {
		r := fmt.Sprintf("%s - %s", schema.ClockTime(t.GetStart()).Format(true), schema.ClockTime(t.GetEnd()).Format(true))
		switch {
		case t.GetOpenStart():
			r = "until " + schema.ClockTime(t.GetEnd()).Format(true)
		case t.GetOpenEnd():
			r = "from " + schema.ClockTime(t.GetStart()).Format(true)
		}
		parts = append(parts, r)
	}
	if rel := t.GetRelation(); rel != epb.TimeAssoc_RELATION_UNSPECIFIED {
		parts = append(parts, "rel="+strings.ToLower(rel.String()))
	}
	if s := t.GetSlots(); len(s) > 0 {
		parts = append(parts, "slots: "+strings.Join(s, "; "))
	}
	return strings.Join(parts, " · ")
}

// issuesEffectsText renders the effect list and reports whether any effect
// kind is unknown to this consumer (from a newer schema).
func issuesEffectsText(effects []*epb.Effect) (string, bool) {
	var parts []string
	var unknown bool
	for _, e := range effects {
		switch e.WhichEffect() {
		case epb.Effect_Cancelled_case:
			parts = append(parts, "cancelled")
		case epb.Effect_Added_case:
			parts = append(parts, "added")
		case epb.Effect_TimeChange_case:
			parts = append(parts, "time-change")
		case epb.Effect_Closure_case:
			parts = append(parts, "closure")
		case epb.Effect_SeasonalHours_case:
			parts = append(parts, "seasonal-hours")
		case epb.Effect_ModifiedHours_case:
			parts = append(parts, "modified-hours")
		case epb.Effect_Restriction_case:
			parts = append(parts, "restriction("+e.GetRestriction().GetText()+")")
		case epb.Effect_SeeSchedule_case:
			parts = append(parts, "see-schedule("+e.GetSeeSchedule().GetName()+")")
		default:
			parts = append(parts, "UNKNOWN")
			unknown = true
		}
	}
	return strings.Join(parts, ", "), unknown
}

const issuesPageCSS = `<style>
body { font: 13px/1.45 system-ui, sans-serif; color: #222; background: #fff;
  margin: 0 auto; padding: 8px 16px 64px; max-width: 1100px; }
h1 { margin: 0; font-size: 15px; }
.meta p, p.meta { margin: 0; color: #666; }
h2 { margin: 22px 0 2px; font-size: 14px; border-bottom: 1px solid #ccc; }
h2 a { color: inherit; }
h3 { margin: 10px 0 2px; font-size: 12px; color: #555; text-transform: uppercase; letter-spacing: 0.03em; }
.n { color: #999; font-weight: normal; font-size: 11px; }
ul { margin: 0; padding-left: 20px; }
li { margin: 1px 0; }
.ctx { color: #888; font-size: 11px; }
code { font: 11px/1.4 ui-monospace, monospace; color: #875; overflow-wrap: anywhere; }
.filters { column-width: 26em; column-gap: 24px; margin: 2px 0; }
.filters label { display: block; font-size: 12px; break-inside: avoid; user-select: none; }
.fbtn { font: 11px system-ui, sans-serif; color: #555; background: none;
  border: 1px solid #ccc; border-radius: 3px; padding: 0 6px; cursor: pointer;
  text-transform: none; letter-spacing: normal; }
.card { border: 1px solid #ddd; padding: 2px 6px; margin: 3px 0; }
.cardhead { color: #555; font-size: 11px; }
.badge { font-weight: bold; }
.badge.bad { color: #a00; }
.badge.warn { color: #a60; }
.row { margin: 0; }
.lbl { display: inline-block; min-width: 78px; color: #999; font-size: 11px; vertical-align: top; }
.block { margin: 2px 0; }
.blockhead { color: #666; font-size: 11px; margin-top: 4px; }
pre { margin: 2px 0; padding: 4px; border: 1px solid #ddd; white-space: pre-wrap;
  overflow-wrap: break-word; font: 11px/1.5 ui-monospace, monospace; }
table.stats { border-collapse: collapse; font-size: 11px; }
.stats td { padding: 0 8px 0 0; text-align: right; }
.stats td + td { text-align: left; color: #555; }
</style>
`

const issuesPageJS = `<script>
// the filter checkboxes hide items by kind; a multi-kind item (e.g. an
// enrichment card with several flags) stays visible while any of its kinds
// is checked. subsections and facility sections with nothing left collapse.
const boxes = [...document.querySelectorAll('.filters input')]
const update = () => {
  const on = new Set(boxes.filter(b => b.checked).map(b => b.value))
  for (const el of document.querySelectorAll('[data-kind]'))
    el.hidden = !el.dataset.kind.split(' ').some(k => on.has(k))
  for (const sub of document.querySelectorAll('.sub'))
    sub.hidden = !sub.querySelector('[data-kind]:not([hidden])')
  for (const sec of document.querySelectorAll('section'))
    sec.hidden = !sec.querySelector('.sub:not([hidden])')
}
for (const box of boxes) box.addEventListener('change', update)
const setAll = v => { for (const box of boxes) box.checked = v; update() }
document.getElementById('fall').addEventListener('click', () => setAll(true))
document.getElementById('fnone').addEventListener('click', () => setAll(false))
document.getElementById('freset').addEventListener('click', () => {
  for (const box of boxes) box.checked = box.defaultChecked
  update()
})
update()
</script>
`
