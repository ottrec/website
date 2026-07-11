package templates

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/ottrec/website/pkg/ottrecidx"
	"github.com/ottrec/website/pkg/ottrecql"
)

// SchedulesSearchQuery returns the ottrecql AST for the schedules page search
// box, fuzzily matching the activity or facility name against q.
func SchedulesSearchQuery(q string) ottrecql.Node {
	return &ottrecql.OrNode{
		Left:  &ottrecql.ActivityNode{FuzzyName: []string{q}},
		Right: &ottrecql.FacilityNode{FuzzyName: []string{q}},
	}
}

// SchedulesCategoryTip returns the category landing page to suggest under the
// simple search box: the one containing every activity in the filtered results,
// as long as they span more than one facility. When nested categories both
// qualify (e.g. lane-swim within swimming), the more specific one wins.
func SchedulesCategoryTip(filtered ottrecidx.DataRef) (ScheduleCategory, bool) {
	mask, nfac, nact := -1, 0, 0
	for fac := range filtered.Facilities() {
		nfac++
		for act := range fac.Activities() {
			nact++
			if mask &= activityCategoryMask(act.GetName()); mask == 0 {
				return ScheduleCategory{}, false
			}
		}
	}
	mask &^= 1 << len(ScheduleCategories) // all-other results get no tip
	if nfac < 2 || nact == 0 || mask == 0 {
		return ScheduleCategory{}, false
	}
	best := -1
	for c, cat := range ScheduleCategories {
		if mask&(1<<c) != 0 && (best < 0 || categoryRefines(cat, ScheduleCategories[best])) {
			best = c
		}
	}
	return ScheduleCategories[best], true
}

// SchedulesMaxQueryLen and SchedulesMaxQueryCost limit user-specified queries
// (see the ottrecql package docs).
const (
	SchedulesMaxQueryLen  = 5000
	SchedulesMaxQueryCost = 7000
)

// SchedulesParseQuery parses a user-specified advanced query, limiting its
// length and cost.
func SchedulesParseQuery(q string) (ottrecql.Node, error) {
	if len(q) > SchedulesMaxQueryLen {
		return nil, fmt.Errorf("query too long (max %d bytes)", SchedulesMaxQueryLen)
	}
	node, err := ottrecql.Parse(q)
	if err != nil {
		return nil, err
	}
	node = ottrecql.Optimize(node)
	if c := ottrecql.Cost(node); c > SchedulesMaxQueryCost {
		return nil, fmt.Errorf("query too expensive (cost %d, max %d)", c, SchedulesMaxQueryCost)
	}
	return node, nil
}

// schedulesAdvancedHref returns the link switching the search into advanced
// mode, translating the current simple search into the underlying query.
func schedulesAdvancedHref(q string, list bool) string {
	href := "/schedules?advanced=1"
	if list {
		href += "&mode=list"
	}
	if q != "" {
		href += "&q=" + url.QueryEscape(ottrecql.Render(SchedulesSearchQuery(q)))
	}
	return href
}

// scheduleTodayHref returns the /today feed link pre-filtered to a category,
// using the shared f-cat filter param keyed by the category's display name.
func scheduleTodayHref(cat ScheduleCategory) string {
	return "/today?f-cat=" + url.QueryEscape(cat.Name)
}

// schedulesNavHref returns the link to another schedules page, preserving the
// current view mode.
func schedulesNavHref(path string, list bool) string {
	if list {
		return path + "?mode=list"
	}
	return path
}

// schedulesViewHref returns the link switching the current page between the
// table and list views, preserving the current search.
func schedulesViewHref(params WebsiteSchedulesParams, list bool) string {
	v := url.Values{}
	if params.Advanced {
		v.Set("advanced", "1")
	}
	if params.Query != "" {
		v.Set("q", params.Query)
	}
	if list {
		v.Set("mode", "list")
	}
	href := params.Path
	if e := v.Encode(); e != "" {
		href += "?" + e
	}
	return href
}

// SchedulesFilter compiles the query and returns the filtered data, with empty
// facilities/groups/schedules/activities elided.
func SchedulesFilter(data ottrecidx.DataRef, query ottrecql.Node) (ottrecidx.DataRef, error) {
	expr, err := ottrecql.Compile(query, nil)
	if err != nil {
		return data, err
	}
	return expr.Filter(data), nil
}

// SchedulesElide returns the data with empty items elided.
func SchedulesElide(data ottrecidx.DataRef) ottrecidx.DataRef {
	mut := data.Mutate()
	mut.Elide()
	return mut.Data()
}

// MapFacilitySlugger returns a function assigning the same slugs as
// [MapFacilityBySlug] to facility names taken in document order. It works on
// data filtered from full so the slugs remain stable, as long as the relative
// facility order is preserved.
func MapFacilitySlugger(full ottrecidx.DataRef) func(name string) string {
	seen := map[string]bool{}
	byName := map[string][]string{}
	for fac := range full.Facilities() {
		name := fac.GetName()
		byName[name] = append(byName[name], mapUniqueSlug(seen, name))
	}
	return func(name string) string {
		s := byName[name]
		if len(s) == 0 {
			return ""
		}
		byName[name] = s[1:]
		return s[0]
	}
}

// SchedulesTOCGroup is a schedule group anchor in the schedules page sidebar.
type SchedulesTOCGroup struct {
	ID    string
	Title string
}

// SchedulesTOCFacility is a facility to render on a schedules page, along with
// its sidebar table-of-contents anchors.
type SchedulesTOCFacility struct {
	Slug   string
	Name   string
	Ref    ottrecidx.FacilityRef
	Groups []SchedulesTOCGroup
}

// facilityAnchorID returns the anchor id for the i'th schedule group (in
// document order) of the facility with the given slug.
func facilityAnchorID(slug string, i int) string {
	return slug + "-g" + strconv.Itoa(i)
}

func schedulesFacilityTOC(slug string, fac ottrecidx.FacilityRef) SchedulesTOCFacility {
	f := SchedulesTOCFacility{Slug: slug, Name: fac.GetName(), Ref: fac}
	for i, grp := range indexedSeq(fac.ScheduleGroups()) {
		f.Groups = append(f.Groups, SchedulesTOCGroup{ID: facilityAnchorID(slug, i), Title: grp.GetTitle()})
	}
	return f
}

// SchedulesTOC collects the facilities of (filtered) data for a schedules
// page, assigning page slugs with slugger (see [MapFacilitySlugger]).
func SchedulesTOC(data ottrecidx.DataRef, slugger func(string) string) []SchedulesTOCFacility {
	var toc []SchedulesTOCFacility
	for fac := range data.Facilities() {
		toc = append(toc, schedulesFacilityTOC(slugger(fac.GetName()), fac))
	}
	return toc
}

// SchedulesFacilityTOC is [SchedulesTOC] for a single-facility page.
func SchedulesFacilityTOC(slug string, fac ottrecidx.FacilityRef) []SchedulesTOCFacility {
	return []SchedulesTOCFacility{schedulesFacilityTOC(slug, fac)}
}

// WebsiteSchedulesParams parameterizes the shared schedules page template for
// the root, category, and single-facility variants.
type WebsiteSchedulesParams struct {
	Base            string
	Data            ottrecidx.DataRef // only used for the data timestamp
	Canonical       string            // canonical path relative to Base, e.g. "schedules"
	Path            string            // current page path, for the view/search mode links
	Active          string            // active category navbar entry ("all" or a category slug; "" for none)
	Title           string
	Description     string // page subtitle, and the meta description unless MetaDescription is set
	MetaDescription string
	Search          bool                   // show the search box
	Advanced        bool                   // advanced (ottrecql) search mode
	Query           string                 // current search box contents
	QueryError      string                 // query parse/limit error to show instead of results
	CategoryTip     *ScheduleCategory      // simple search: landing page tip (see SchedulesCategoryTip)
	Single          bool                   // single-facility page: hide the page header and facility page links
	List            bool                   // compact list view (?mode=list) instead of the schedule tables
	NoIndex         bool                   // emit a noindex robots meta (e.g. the per-activity /all full views)
	CategoryTerms   []string               // category pages: the activity names used for filtering, for the incompleteness note
	TOC             []SchedulesTOCFacility // facilities to render, with their sidebar anchors
}
