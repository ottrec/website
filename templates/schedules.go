package templates

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/pgaskin/ottrec-website/pkg/ottrecidx"
	"github.com/pgaskin/ottrec-website/pkg/ottrecql"
)

// ScheduleCategory defines a category page at /schedules/{Slug}, matching
// activities by ottrecql fuzzy names. Icon is a Material Symbols name mapped
// to a glyph in home.css (the glyph must be included in static/fonts.go).
type ScheduleCategory struct {
	Slug        string
	Name        string
	Description string
	Icon        string
	Activities  []string
}

// ScheduleCategories contains the predefined schedule category pages, shown in
// the schedules category navbar. It mirrors the map filter categories
// ([mapCategories]), but matches with ottrecql fuzzy activity names instead of
// regexps.
var ScheduleCategories = []ScheduleCategory{
	{"swimming", "Swimming", "All swims, including public, lane, and wave swims.", "pool", []string{"swim"}},
	{"lane-swim", "Lane Swim", "Lane swims.", "pool", []string{"lane swim"}},
	{"aquafit", "Aquafit", "Aquafit and aqua lite.", "water", []string{"aqua"}},
	{"skating", "Skating", "Public, adult, family, senior, and figure skating.", "ice_skating", []string{"skat"}},
	{"hockey", "Hockey", "Hockey, shinny, stick and puck, and ringette.", "sports_hockey", []string{"hockey", "shinny", "stick and puck", "ringette"}},
	{"badminton", "Badminton", "Badminton.", "badminton", []string{"badminton"}},
	{"basketball", "Basketball", "Basketball.", "sports_basketball", []string{"basketball"}},
	{"volleyball", "Volleyball", "Volleyball.", "sports_volleyball", []string{"volleyball"}},
	{"pickleball", "Pickleball", "Pickleball.", "pickleball", []string{"pickleball"}},
}

// ScheduleCategoryBySlug resolves a slug from a schedules page path.
func ScheduleCategoryBySlug(slug string) (ScheduleCategory, bool) {
	for _, cat := range ScheduleCategories {
		if cat.Slug == slug {
			return cat, true
		}
	}
	return ScheduleCategory{}, false
}

// Query returns the ottrecql AST matching the category's activities.
func (c ScheduleCategory) Query() ottrecql.Node {
	return &ottrecql.ActivityNode{FuzzyName: c.Activities}
}

// SchedulesSearchQuery returns the ottrecql AST for the schedules page search
// box, fuzzily matching the activity or facility name against q.
func SchedulesSearchQuery(q string) ottrecql.Node {
	return &ottrecql.OrNode{
		Left:  &ottrecql.ActivityNode{FuzzyName: []string{q}},
		Right: &ottrecql.FacilityNode{FuzzyName: []string{q}},
	}
}

// SchedulesMaxQueryLen and SchedulesMaxQueryCost limit user-specified queries
// (see the ottrecql package docs).
const (
	SchedulesMaxQueryLen  = 2000
	SchedulesMaxQueryCost = 5000
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
	Search        bool                   // show the search box
	Advanced      bool                   // advanced (ottrecql) search mode
	Query         string                 // current search box contents
	QueryError    string                 // query parse/limit error to show instead of results
	Single        bool                   // single-facility page: hide the page header and facility page links
	List          bool                   // compact list view (?mode=list) instead of the schedule tables
	CategoryTerms []string               // category pages: the activity names used for filtering, for the incompleteness note
	TOC           []SchedulesTOCFacility // facilities to render, with their sidebar anchors
}
