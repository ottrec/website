package ottrecql

import (
	"slices"
	"time"

	"github.com/pgaskin/ottrec-website/pkg/ottrecidx"
	"github.com/pgaskin/ottrec/schema"
)

// TODO: optimize expression by swapping sides of and/or by cost

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
	now     time.Time
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
		now:     now,
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
		var t time.Time
		if e.Date.IsToday {
			t = c.now
		} else {
			t = time.Date(e.Date.Year, e.Date.Month, e.Date.Day, 0, 0, 0, 0, ottrecidx.TZ)
		}
		return &cSchDate{t: t}, nil
	case *TimeNode:
		ds := make([]cTimeDate, len(e.Days))
		for i, d := range e.Days {
			switch d := d.(type) {
			case DateLit:
				if d.IsToday {
					ds[i].HasDate = true
					ds[i].Year, ds[i].Month, ds[i].Day = c.now.Date()
					ds[i].Weekday = c.now.Weekday()
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
					hh, mm, _ := c.now.Clock()
					ct = append(ct, schema.MakeClockTime(hh, mm))
				} else {
					ct = append(ct, schema.MakeClockTime(t.Hour, t.Minute))
				}
			case TimeRangeLit:
				hh1, mm1 := t.Start.Hour, t.Start.Minute
				if t.Start.IsNow {
					hh1, mm1, _ = c.now.Clock()
				}
				hh2, mm2 := t.End.Hour, t.End.Minute
				if t.End.IsNow {
					hh2, mm2, _ = c.now.Clock()
				}
				cr = append(cr, schema.MakeClockRange(hh1, mm1, hh2, mm2))
			default:
				panic("invalid time spec")
			}
		}
		return &cTime{d: ds, ct: ct, cr: cr}, nil
	case *FacilityNode:
		return &cFacility{m: c.fuzzies(e.FuzzyName)}, nil
	case *ActivityNode:
		return &cActivity{m: c.fuzzies(e.FuzzyName)}, nil
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
		return e.root.evalFacility(ref).matches()
	})
	mut.FilterSchedules(func(ref ottrecidx.ScheduleRef) bool {
		return e.root.evalSchedule(ref).matches()
	})
	mut.FilterActivities(func(ref ottrecidx.ActivityRef) bool {
		return e.root.evalActivity(ref).matches()
	})
	mut.FilterTimes(func(ref ottrecidx.TimeRef) bool {
		return e.root.eval(ref).matches()
	})

	// remove empty items
	mut.ElideActivities()
	mut.ElideSchedules()
	mut.ElideScheduleGroups()
	mut.ElideFacilities()

	return mut.Data()
}

// result represents a potentially unknown boolean value.
type result byte

const (
	rUnknown result = 0
	rFalse   result = 'f'
	rTrue    result = 't'
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
	eval(tm ottrecidx.TimeRef) result              // full evaluation
	evalActivity(act ottrecidx.ActivityRef) result // short-circuit evaluation against an activity
	evalSchedule(sch ottrecidx.ScheduleRef) result // short-circuit evaluation against a schedule
	evalFacility(fac ottrecidx.FacilityRef) result // short-circuit evaluation against a facility
}

type cNot struct {
	a cNode
}

func (n *cNot) eval(tm ottrecidx.TimeRef) result {
	return evalNot(tm, n.a.eval)
}

func (n *cNot) evalActivity(act ottrecidx.ActivityRef) result {
	return evalNot(act, n.a.evalActivity)
}

func (n *cNot) evalSchedule(sch ottrecidx.ScheduleRef) result {
	return evalNot(sch, n.a.evalSchedule)
}

func (n *cNot) evalFacility(fac ottrecidx.FacilityRef) result {
	return evalNot(fac, n.a.evalFacility)
}

type cAnd struct {
	a cNode
	b cNode
}

func (n *cAnd) eval(tm ottrecidx.TimeRef) result {
	return evalAnd(tm, n.a.eval, n.b.eval)
}

func (n *cAnd) evalActivity(act ottrecidx.ActivityRef) result {
	return evalAnd(act, n.a.evalActivity, n.b.evalActivity)
}

func (n *cAnd) evalSchedule(sch ottrecidx.ScheduleRef) result {
	return evalAnd(sch, n.a.evalSchedule, n.b.evalSchedule)
}

func (n *cAnd) evalFacility(fac ottrecidx.FacilityRef) result {
	return evalAnd(fac, n.a.evalFacility, n.b.evalFacility)
}

type cOr struct {
	a cNode
	b cNode
}

func (n *cOr) eval(tm ottrecidx.TimeRef) result {
	return evalOr(tm, n.a.eval, n.b.eval)
}

func (n *cOr) evalActivity(act ottrecidx.ActivityRef) result {
	return evalOr(act, n.a.evalActivity, n.b.evalActivity)
}

func (n *cOr) evalSchedule(sch ottrecidx.ScheduleRef) result {
	return evalOr(sch, n.a.evalSchedule, n.b.evalSchedule)
}

func (n *cOr) evalFacility(fac ottrecidx.FacilityRef) result {
	return evalOr(fac, n.a.evalFacility, n.b.evalFacility)
}

type cSchDate struct {
	t time.Time
}

func (n *cSchDate) eval(tm ottrecidx.TimeRef) result {
	r := n.evalScheduleOnly(tm.Schedule())
	if r == rFalse {
		// if the schedule date doesn't match but we included it since we have a
		// activity time exactly for the requested date, include it only if it's
		// that time
		if t, ok := tm.SingleDate(); ok {
			y1, m1, d1 := n.t.Date()
			y2, m2, d2 := t.Date()
			if y1 == y2 && m1 == m2 && d1 == d2 {
				r = rTrue
			}
		}
	}
	return r
}

func (n *cSchDate) evalActivity(act ottrecidx.ActivityRef) result {
	return n.evalSchedule(act.Schedule())
}

func (n *cSchDate) evalSchedule(sch ottrecidx.ScheduleRef) result {
	r := n.evalScheduleOnly(sch)
	if r == rFalse {
		// if the schedule date doesn't match but we have a activity time
		// exactly for the requested date, include it
		for tm := range sch.Times() {
			if t, ok := tm.SingleDate(); ok {
				y1, m1, d1 := n.t.Date()
				y2, m2, d2 := t.Date()
				if y1 == y2 && m1 == m2 && d1 == d2 {
					r = rTrue
				}
			}
		}
	}
	return r
}

func (n *cSchDate) evalScheduleOnly(sch ottrecidx.ScheduleRef) result {
	from, to, ok := sch.ComputeEffectiveDateRange()
	if !ok {
		return rUnknown // don't let an unknown schedule date be the thing which causes something to be removed
	}
	if !from.IsZero() && n.t.Before(from) {
		return rFalse
	}
	if !to.IsZero() && n.t.After(to) {
		return rFalse
	}
	return rTrue
}

func (n *cSchDate) evalFacility(fac ottrecidx.FacilityRef) result { return rUnknown }

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

func (n *cTime) eval(tm ottrecidx.TimeRef) result {
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
				if yyyy, mm, dd := sd.Date(); yyyy == d.Year && mm == d.Month && dd == d.Day {
					return rTrue
				}
			} else {
				// single day, but we only have a weekday, so check if the date weekday matches
				if d.Weekday == sd.Weekday() {
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

func (n *cTime) evalActivity(act ottrecidx.ActivityRef) result { return rUnknown }
func (n *cTime) evalSchedule(sch ottrecidx.ScheduleRef) result { return rUnknown }
func (n *cTime) evalFacility(fac ottrecidx.FacilityRef) result { return rUnknown }

type cFacility struct {
	m []func(string) bool
}

func (n *cFacility) eval(tm ottrecidx.TimeRef) result {
	return n.evalFacility(tm.Facility())
}

func (n *cFacility) evalActivity(act ottrecidx.ActivityRef) result {
	return n.evalFacility(act.Facility())
}

func (n *cFacility) evalSchedule(sch ottrecidx.ScheduleRef) result {
	return n.evalFacility(sch.Facility())
}

func (n *cFacility) evalFacility(fac ottrecidx.FacilityRef) result {
	for _, m := range n.m {
		if m(fac.GetName()) {
			return rTrue
		}
	}
	return rFalse
}

type cActivity struct {
	m []func(string) bool
}

func (n *cActivity) eval(tm ottrecidx.TimeRef) result {
	return n.evalActivity(tm.Activity())
}

func (n *cActivity) evalActivity(act ottrecidx.ActivityRef) result {
	for _, m := range n.m {
		if m(act.GetName()) || m(act.GetLabel()) {
			return rTrue
		}
	}
	return rFalse
}

func (n *cActivity) evalSchedule(sch ottrecidx.ScheduleRef) result { return rUnknown }
func (n *cActivity) evalFacility(fac ottrecidx.FacilityRef) result { return rUnknown }

type cLatLng struct {
	lat  float64
	lng  float64
	dist float64
}

func (n *cLatLng) eval(tm ottrecidx.TimeRef) result {
	return n.evalFacility(tm.Facility())
}

func (n *cLatLng) evalActivity(act ottrecidx.ActivityRef) result {
	return n.evalFacility(act.Facility())
}

func (n *cLatLng) evalSchedule(sch ottrecidx.ScheduleRef) result {
	return n.evalFacility(sch.Facility())
}

func (n *cLatLng) evalFacility(fac ottrecidx.FacilityRef) result {
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
