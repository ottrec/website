package ottrecql

import (
	"cmp"
	"slices"
	"time"

	"github.com/pgaskin/ottrec-website/pkg/ottrecidx"
	"github.com/pgaskin/ottrec/schema"
)

// TODO: optimize expression by swapping sides of and/or by cost

// TODO: optimize expression by deduplicating nodes, then storing last ref and result in each one, then returning that if matches

type Context struct {
	// Now, if non-zero, overrides the date used for the expression.
	Now time.Time
}

func Compile(e Node, c *Context) (*Expr, error) {
	root, err := newCompileCtx(c).compile(e)
	if err != nil {
		return nil, err
	}
	return &Expr{
		root: root,
	}, nil
}

type compileCtx struct {
	today   schema.Date
	now     schema.ClockTime
	fuzzy   map[string]func(string) bool
	mkfuzzy func(string) func(string) bool
}

func newCompileCtx(c *Context) *compileCtx {
	if c == nil {
		c = new(Context)
	}
	now := c.Now
	if now.IsZero() {
		now = time.Now()
	}
	now = now.In(ottrecidx.TZ)
	cc := &compileCtx{
		today:   schema.MakeDateFromGo(now),
		now:     schema.MakeClockTime(now.Hour(), now.Minute()),
		fuzzy:   make(map[string]func(string) bool),
		mkfuzzy: newFuzzyWordMatcher(),
	}
	return cc
}

func (c *compileCtx) compile(e Node) (cNode, error) {
	switch e := e.(type) {
	case *NotNode:
		expr, err := c.compile(e.Expr)
		if err != nil {
			return nil, err
		}
		return &cNot{a: expr}, nil
	case *AndNode:
		left, err := c.compile(e.Left)
		if err != nil {
			return nil, err
		}
		right, err := c.compile(e.Right)
		if err != nil {
			return nil, err
		}
		return &cAnd{a: left, b: right}, nil
	case *OrNode:
		left, err := c.compile(e.Left)
		if err != nil {
			return nil, err
		}
		right, err := c.compile(e.Right)
		if err != nil {
			return nil, err
		}
		return &cOr{a: left, b: right}, nil
	case *SchDateNode:
		var t schema.Date
		if e.Date.IsToday {
			t = c.today
		} else {
			t = schema.MakeDate(e.Date.Year, e.Date.Month, e.Date.Day, -1)
		}
		return &cSchDate{t: t}, nil
	case *TimeNode:
		ds := make([]cTimeDate, len(e.Days))
		for i, d := range e.Days {
			switch d := d.(type) {
			case DateLit:
				if d.IsToday {
					ds[i].HasDate = true
					ds[i].Year, _ = c.today.Year()
					ds[i].Month, _ = c.today.Month()
					ds[i].Day, _ = c.today.Day()
					ds[i].Weekday, _ = c.today.Weekday()
				} else {
					ds[i].Weekday = time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, ottrecidx.TZ).Weekday()
				}
			case WeekdayLit:
				ds[i].Weekday = d.Weekday
			default:
				panic("invalid day spec")
			}
		}
		ct := make([]schema.ClockTime, 0, len(e.Times))
		cr := make([]schema.ClockRange, 0, len(e.Times))
		for _, t := range e.Times {
			switch t := t.(type) {
			case TimeLit:
				if t.IsNow {
					ct = append(ct, c.now)
				} else {
					ct = append(ct, schema.MakeClockTime(t.Hour, t.Minute))
				}
			case TimeRangeLit:
				hh1, mm1 := t.Start.Hour, t.Start.Minute
				if t.Start.IsNow {
					_, hh1, mm1 = c.now.Split()
				}
				hh2, mm2 := t.End.Hour, t.End.Minute
				if t.End.IsNow {
					_, hh2, mm2 = c.now.Split()
				}
				cr = append(cr, schema.MakeClockRange(hh1, mm1, hh2, mm2))
			default:
				panic("invalid time spec")
			}
		}
		return &cTime{d: ds, ct: ct, cr: cr}, nil
	case *FacilityNode:
		var n []cNode
		for _, m := range c.fuzzies(e.FuzzyName) {
			n = append(n, &cFacility{
				m: m,
			})
		}
		if len(n) == 1 {
			return n[0], nil
		}
		return &cFlatOr{n: n}, nil
	case *ActivityNode:
		var n []cNode
		for _, m := range c.fuzzies(e.FuzzyName) {
			n = append(n, &cActivity{
				m: m,
			})
		}
		if len(n) == 1 {
			return n[0], nil
		}
		return &cFlatOr{n: n}, nil
	case *LatLngNode:
		return &cLatLng{lat: float64(e.Lat), lng: float64(e.Lng), dist: float64(e.Dist)}, nil
	default:
		panic("invalid node")
	}
}

func (c *compileCtx) fuzzies(s []string) []func(string) bool {
	ms := make([]func(string) bool, len(s))
	for i, s := range s {
		m, ok := c.fuzzy[s]
		if !ok {
			m = c.mkfuzzy(s)
			c.fuzzy[s] = m
		}
		ms[i] = m
	}
	return ms
}

// Expr represents a prepared expression. It must not be used concurrently.
type Expr struct {
	root cNode
}

// Filter filters data, returning the filtered copy.
func (e *Expr) Filter(data ottrecidx.DataRef) ottrecidx.DataRef {
	mut := data.Mutate()

	// filter, doing the top-level stuff first as an optimization
	mut.FilterFacilities(func(ref ottrecidx.FacilityRef) bool {
		return e.root.eval1(ref).matches()
	})
	mut.FilterSchedules(func(ref ottrecidx.ScheduleRef) bool {
		return e.root.eval2(ref).matches()
	})
	mut.FilterActivities(func(ref ottrecidx.ActivityRef) bool {
		return e.root.eval3(ref).matches()
	})
	mut.FilterTimes(func(ref ottrecidx.TimeRef) bool {
		return e.root.eval4(ref).matches()
	})

	// remove empty items
	mut.ElideActivities()
	mut.ElideSchedules()
	mut.ElideScheduleGroups()
	mut.ElideFacilities()

	return mut.Data()
}

// result represents a potentially unknown boolean value.
//
// The [rUnknown]/[rNotApplicable] values work because they prevents that clause
// of the query from being the reason something is filtered out, and if
// something else would filter it out, it is short-circuited. The [cNode.eval4]
// method must not return [rNotApplicable] otherwise filters ANDing multiple
// levels like `(activity("lane") and facility("bob")) and not (activity("lane")
// and facility("bob"))` will not work correctly.
type result byte

const (
	rUnknown       result = 0
	rNotApplicable result = rUnknown
	rFalse         result = 'f'
	rTrue          result = 't'
)

// matches returns true if result is unknown or true.
func (r result) matches() bool {
	return r != rFalse
}

// evalNot evaluates a NOT expression.
func evalNot[T any](x T, a func(T) result) result {
	ra := a(x)
	if ra == rFalse {
		return rTrue
	}
	if ra == rTrue {
		return rFalse
	}
	return ra
}

// evalOr evaluates an AND expression, short-circuiting b if possible.
func evalAnd[T any](x T, a func(T) result, b func(T) result) result {
	ra := a(x)
	if ra == rFalse {
		return rFalse
	}
	rb := b(x)
	if rb == rFalse {
		return rFalse
	}
	if ra == rUnknown || rb == rUnknown {
		return rUnknown
	}
	return rTrue
}

// evalOr evaluates an OR expression, short-circuiting b if possible.
func evalOr[T any](x T, a func(T) result, b func(T) result) result {
	ra := a(x)
	if ra == rTrue {
		return rTrue
	}
	rb := b(x)
	if rb == rTrue {
		return rTrue
	}
	if ra == rUnknown || rb == rUnknown {
		return rUnknown
	}
	return rFalse
}

type cNode interface {
	eval4(tm ottrecidx.TimeRef) result      // full evaluation
	eval3(act ottrecidx.ActivityRef) result // short-circuit evaluation against an activity
	eval2(sch ottrecidx.ScheduleRef) result // short-circuit evaluation against a schedule
	eval1(fac ottrecidx.FacilityRef) result // short-circuit evaluation against a facility
}

type cNot struct {
	a cNode
}

func (n *cNot) eval4(tm ottrecidx.TimeRef) result {
	return evalNot(tm, n.a.eval4)
}

func (n *cNot) eval3(act ottrecidx.ActivityRef) result {
	return evalNot(act, n.a.eval3)
}

func (n *cNot) eval2(sch ottrecidx.ScheduleRef) result {
	return evalNot(sch, n.a.eval2)
}

func (n *cNot) eval1(fac ottrecidx.FacilityRef) result {
	return evalNot(fac, n.a.eval1)
}

type cAnd struct {
	a cNode
	b cNode
}

func (n *cAnd) eval4(tm ottrecidx.TimeRef) result {
	return evalAnd(tm, n.a.eval4, n.b.eval4)
}

func (n *cAnd) eval3(act ottrecidx.ActivityRef) result {
	return evalAnd(act, n.a.eval3, n.b.eval3)
}

func (n *cAnd) eval2(sch ottrecidx.ScheduleRef) result {
	return evalAnd(sch, n.a.eval2, n.b.eval2)
}

func (n *cAnd) eval1(fac ottrecidx.FacilityRef) result {
	return evalAnd(fac, n.a.eval1, n.b.eval1)
}

type cOr struct {
	a cNode
	b cNode
}

func (n *cOr) eval4(tm ottrecidx.TimeRef) result {
	return evalOr(tm, n.a.eval4, n.b.eval4)
}

func (n *cOr) eval3(act ottrecidx.ActivityRef) result {
	return evalOr(act, n.a.eval3, n.b.eval3)
}

func (n *cOr) eval2(sch ottrecidx.ScheduleRef) result {
	return evalOr(sch, n.a.eval2, n.b.eval2)
}

func (n *cOr) eval1(fac ottrecidx.FacilityRef) result {
	return evalOr(fac, n.a.eval1, n.b.eval1)
}

type cFlatOr struct {
	n []cNode
}

func (n *cFlatOr) eval4(tm ottrecidx.TimeRef) result {
	var unk bool
	if len(n.n) == 0 {
		return rTrue
	}
	for _, n := range n.n {
		switch n.eval4(tm) {
		case rTrue:
			return rTrue
		case rFalse:
			if unk {
				return rUnknown
			}
		case rUnknown:
			unk = true
		}
	}
	if unk {
		return rUnknown
	}
	return rFalse
}

func (n *cFlatOr) eval3(act ottrecidx.ActivityRef) result {
	var unk bool
	if len(n.n) == 0 {
		return rTrue
	}
	for _, n := range n.n {
		switch n.eval3(act) {
		case rTrue:
			return rTrue
		case rFalse:
			if unk {
				return rUnknown
			}
		case rUnknown:
			unk = true
		}
	}
	if unk {
		return rUnknown
	}
	return rFalse
}

func (n *cFlatOr) eval2(sch ottrecidx.ScheduleRef) result {
	var unk bool
	if len(n.n) == 0 {
		return rTrue
	}
	for _, n := range n.n {
		switch n.eval2(sch) {
		case rTrue:
			return rTrue
		case rFalse:
			if unk {
				return rUnknown
			}
		case rUnknown:
			unk = true
		}
	}
	if unk {
		return rUnknown
	}
	return rFalse
}

func (n *cFlatOr) eval1(fac ottrecidx.FacilityRef) result {
	var unk bool
	if len(n.n) == 0 {
		return rTrue
	}
	for _, n := range n.n {
		switch n.eval1(fac) {
		case rTrue:
			return rTrue
		case rUnknown:
			unk = true
		}
	}
	if unk {
		return rUnknown
	}
	return rFalse
}

type cSchDate struct {
	t schema.Date
}

func (n *cSchDate) eval4(tm ottrecidx.TimeRef) result      { return n.eval2(tm.Schedule()) }
func (n *cSchDate) eval3(act ottrecidx.ActivityRef) result { return rNotApplicable }
func (n *cSchDate) eval1(fac ottrecidx.FacilityRef) result { return rUnknown }

func (n *cSchDate) eval2(sch ottrecidx.ScheduleRef) result {
	r := n.evalScheduleOnly(sch)
	if r == rFalse {
		// if the schedule date doesn't match but we have a activity time
		// exactly for the requested date, include it
		for tm := range sch.Times() {
			if t, ok := tm.SingleDate(); ok {
				// it's all going to be full dates, so no need to check ok
				y1, _ := n.t.Year()
				m1, _ := n.t.Month()
				d1, _ := n.t.Day()
				y2, _ := t.Year()
				m2, _ := t.Month()
				d2, _ := t.Day()
				if y1 == y2 && m1 == m2 && d1 == d2 {
					r = rTrue
				}
			}
		}
	}
	return r
}

func (n *cSchDate) evalScheduleOnly(sch ottrecidx.ScheduleRef) result {
	er, ok := sch.ComputeEffectiveDateRange()
	if !ok {
		return rUnknown // don't let an unknown schedule date be the thing which causes something to be removed
	}
	if !er.From.IsZero() && compareFullDate(n.t, er.From) < 0 {
		return rFalse
	}
	if !er.To.IsZero() && compareFullDate(n.t, er.To) > 0 {
		return rFalse
	}
	return rTrue
}

func compareFullDate(a, b schema.Date) int {
	ay, _ := a.Year()
	by, _ := b.Year()
	if ay != by {
		return cmp.Compare(ay, by)
	}
	am, _ := a.Month()
	bm, _ := b.Month()
	if am != bm {
		return cmp.Compare(am, bm)
	}
	ad, _ := a.Day()
	bd, _ := b.Day()
	return cmp.Compare(ad, bd)
}

type cTime struct {
	d  []cTimeDate
	ct []schema.ClockTime
	cr []schema.ClockRange
}

type cTimeDate struct {
	Weekday time.Weekday

	HasDate bool
	Year    int
	Month   time.Month
	Day     int
}

func (n *cTime) eval3(act ottrecidx.ActivityRef) result { return rUnknown }
func (n *cTime) eval2(sch ottrecidx.ScheduleRef) result { return rUnknown }
func (n *cTime) eval1(fac ottrecidx.FacilityRef) result { return rUnknown }

func (n *cTime) eval4(tm ottrecidx.TimeRef) result {
	return evalAnd(tm, n.dateMatch, n.timeMatch)
}

func (n *cTime) dateMatch(tm ottrecidx.TimeRef) result {
	if len(n.d) == 0 {
		return rTrue
	}
	wd, wdOK := tm.GetWeekday()
	sd, isSingleDate := tm.SingleDate()
	var unk bool
	for _, d := range n.d {
		if isSingleDate {
			if d.HasDate {
				// single day, and we have a full date, so check if it's the same
				yy, _ := sd.Year()
				mm, _ := sd.Month()
				dd, _ := sd.Day()
				if yy == d.Year && mm == d.Month && dd == d.Day {
					return rTrue
				}
			} else {
				// single day, but we only have a weekday, so check if the date weekday matches
				wd, _ := sd.Weekday()
				if d.Weekday == wd {
					return rTrue
				}
			}
		} else {
			// not a single day, so check if the day weekday matches
			if !wdOK {
				unk = true // no valid weekday, so don't make a lack of matches the reason why we exclude this time
			} else if d.Weekday == wd {
				return rTrue
			}
		}
	}
	if unk {
		return rUnknown
	}
	return rFalse
}

func (n *cTime) timeMatch(tm ottrecidx.TimeRef) result {
	if len(n.ct) == 0 && len(n.cr) == 0 {
		return rTrue // no time filter
	}
	cr, ok := tm.GetRange()
	if !ok {
		return rUnknown // no valid time range, so don't make a lack of matches the reason why we exclude this time
	}
	for _, x := range n.ct {
		if cr.Start <= x && x < cr.End {
			return rTrue
		}
	}
	if slices.ContainsFunc(n.cr, cr.Overlaps) {
		return rTrue
	}
	return rFalse
}

type cFacility struct {
	m func(string) bool
}

func (n *cFacility) eval4(tm ottrecidx.TimeRef) result      { return n.eval1(tm.Facility()) }
func (n *cFacility) eval3(act ottrecidx.ActivityRef) result { return rNotApplicable }
func (n *cFacility) eval2(sch ottrecidx.ScheduleRef) result { return rNotApplicable }

func (n *cFacility) eval1(fac ottrecidx.FacilityRef) result {
	if n.m(fac.GetName()) {
		return rTrue
	}
	return rFalse
}

type cActivity struct {
	m func(string) bool
}

func (n *cActivity) eval4(tm ottrecidx.TimeRef) result      { return n.eval3(tm.Activity()) }
func (n *cActivity) eval2(sch ottrecidx.ScheduleRef) result { return rUnknown }
func (n *cActivity) eval1(fac ottrecidx.FacilityRef) result { return rUnknown }

func (n *cActivity) eval3(act ottrecidx.ActivityRef) result {
	if n.m(act.GetName()) || n.m(act.GetLabel()) {
		return rTrue
	}
	return rFalse
}

type cLatLng struct {
	lat  float64
	lng  float64
	dist float64
}

func (n *cLatLng) eval4(tm ottrecidx.TimeRef) result      { return n.eval1(tm.Facility()) }
func (n *cLatLng) eval3(act ottrecidx.ActivityRef) result { return rNotApplicable }
func (n *cLatLng) eval2(sch ottrecidx.ScheduleRef) result { return rNotApplicable }

func (n *cLatLng) eval1(fac ottrecidx.FacilityRef) result {
	lng, lat, ok := fac.GetLngLat()
	if !ok {
		return rTrue
	}
	dist := distanceBetween(float64(lat), float64(lng), n.lat, n.lng)
	if dist <= n.dist {
		return rTrue
	}
	return rFalse
}
