package ottrecidx

import (
	"fmt"
	"iter"
	"math"
	"time"

	"github.com/pgaskin/ottrec/schema"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// this file contains the internal representation of data based on the schema

var (
	_data             = descriptorOf[*schema.Data]()
	_data_facilities  = mustField(_data, "facilities")
	_data_attribution = mustField(_data, "attribution")

	_fac                    = descriptorOf[*schema.Facility]()
	_fac_name               = mustField(_fac, "name")
	_fac_description        = mustField(_fac, "description")
	_fac_source             = mustField(_fac, "source")
	_fac_address            = mustField(_fac, "address")
	_fac_lnglat             = mustField(_fac, "_lnglat")
	_fac_notifications_html = mustField(_fac, "notifications_html")
	_fac_special_hours_html = mustField(_fac, "special_hours_html")
	_fac_schedule_groups    = mustField(_fac, "schedule_groups")
	_fac_errors             = mustField(_fac, "_errors")

	_source      = descriptorOf[*schema.Source]()
	_source_url  = mustField(_source, "url")
	_source_date = mustField(_source, "_date")

	_lnglat     = descriptorOf[*schema.LngLat]()
	_lnglat_lng = mustField(_lnglat, "lng")
	_lnglat_lat = mustField(_lnglat, "lat")

	_grp                       = descriptorOf[*schema.ScheduleGroup]()
	_grp_label                 = mustField(_grp, "label")
	_grp_title                 = mustField(_grp, "_title")
	_grp_schedule_changes_html = mustField(_grp, "schedule_changes_html")
	_grp_schedules             = mustField(_grp, "schedules")
	_grp_reservation_links     = mustField(_grp, "reservation_links")
	_grp_noresv                = mustField(_grp, "_noresv")

	_sch            = descriptorOf[*schema.Schedule]()
	_sch_caption    = mustField(_sch, "caption")
	_sch_name       = mustField(_sch, "_name")
	_sch_date       = mustField(_sch, "_date")
	_sch_from       = mustField(_sch, "_from")
	_sch_to         = mustField(_sch, "_to")
	_sch_days       = mustField(_sch, "days")
	_sch_daydates   = mustField(_sch, "_daydates")
	_sch_activities = mustField(_sch, "activities")

	_act       = descriptorOf[*schema.Schedule_Activity]()
	_act_label = mustField(_act, "label")
	_act_name  = mustField(_act, "_name")
	_act_resv  = mustField(_act, "_resv")
	_act_days  = mustField(_act, "days")

	_day       = descriptorOf[*schema.Schedule_ActivityDay]()
	_day_times = mustField(_day, "times")

	_tm       = descriptorOf[*schema.TimeRange]()
	_tm_label = mustField(_tm, "label")
	_tm_start = mustField(_tm, "_start")
	_tm_end   = mustField(_tm, "_end")
	_tm_wkday = mustField(_tm, "_wkday")

	_lnk       = descriptorOf[*schema.ReservationLink]()
	_lnk_label = mustField(_lnk, "label")
	_lnk_url   = mustField(_lnk, "url")

	_timestamp         = descriptorOf[*timestamppb.Timestamp]()
	_timestamp_seconds = mustField(_timestamp, "seconds")
	_timestamp_nanos   = mustField(_timestamp, "nanos")
)

type schemaObj interface {
	xData | xFacility | xScheduleGroup | xSchedule | xActivity | xTime
}

type xData struct {
	Attribution []string
}

type xFacility struct {
	Name              string
	Description       string
	SourceURL         string
	SourceDate        time.Time
	Address           string
	Longitude         float32 // geocoded (lng+lat zero if not present)
	Latitude          float32 // geocoded (lng+lat zero if not present)
	NotificationsHTML string
	SpecialHoursHTML  string
	Errors            []string
}

type xScheduleGroup struct {
	Label               string
	Title               string // derived
	ReservationLinks    []ReservationLink
	ScheduleChangesHTML string
	NoResv              bool
}

type xSchedule struct {
	Caption   string
	Name      string           // derived
	Date      string           // derived
	DateRange schema.DateRange // derived (from+to zero if not parsed)
	Days      []string
	DayDates  []schema.Date // derived (zero if not parsed)
}

type xActivity struct {
	Label         string
	Name          string // derived
	Resv, HasResv bool   // derived
}

type xTime struct {
	ScheduleDay int // index into xSchedule.Days
	Label       string
	Weekday     time.Weekday      // derived (-1 if not parsed)
	Range       schema.ClockRange // derived (start+end zero if not parsed)
}

// note: don't count simple data holders which:
// 	- are returned directly;
//	- don't need to be able to backref;
//	- and don't have children
// as schema objects since that will add unnecessary complexity and overhead

type ReservationLink struct {
	Label string
	URL   string
}

func newData(a *arena, sa *stringInterner, data protoreflect.Message) *xData {
	x := arenaNew[xData](a)
	x.Attribution = mapList(a, data.Get(_data_attribution), func(m protoreflect.Value) string {
		return sa.InternFast(m.String())
	})
	return x
}

func newFacility(a *arena, sa *stringInterner, fac protoreflect.Message) *xFacility {
	x := arenaNew[xFacility](a)
	x.Name = sa.Intern(optString(fac, _fac_name))
	x.Description = sa.InternFast(optString(fac, _fac_description))
	if src := fac.Get(_fac_source); src.IsValid() {
		x.SourceURL = sa.InternFast(optString(src.Message(), _source_url))
		if v := src.Message().Get(_source_date); v.IsValid() {
			x.SourceDate = time.Unix(int64(v.Message().Get(_timestamp_seconds).Int()), int64(v.Message().Get(_timestamp_nanos).Int())).UTC()
		}
	}
	x.Address = sa.InternFast(fac.Get(_fac_address).String())
	if ll := fac.Get(_fac_lnglat); ll.IsValid() {
		lng := ll.Message().Get(_lnglat_lng)
		lat := ll.Message().Get(_lnglat_lat)
		if lng.IsValid() && lat.IsValid() {
			x.Longitude = float32(lng.Float())
			x.Latitude = float32(lat.Float())
			fixLngLatRegression(&x.Longitude, &x.Latitude)
		}
	}
	x.NotificationsHTML = sa.InternFast(optString(fac, _fac_notifications_html))
	x.SpecialHoursHTML = sa.InternFast(optString(fac, _fac_special_hours_html))
	x.Errors = mapList(a, fac.Get(_fac_errors), func(m protoreflect.Value) string {
		return sa.InternFast(m.String())
	})
	return x
}

// fixLngLatRegression fixes the longitude/latitude mixup in the scraper from
// bc9be9b7098f8daaba3121daa564fcbeb4b85784 (2025-11-18) to
// 6ec0cae178db0f405b6c9451a43e53566e541e22 (2026-03-09).
func fixLngLatRegression(lng, lat *float32) {
	if math.Trunc(float64(*lng)/10) == 4 && math.Trunc(float64(*lat)/10) == -7 {
		*lng, *lat = *lat, *lng
	}
}

func newScheduleGroup(a *arena, sa *stringInterner, grp protoreflect.Message) *xScheduleGroup {
	x := arenaNew[xScheduleGroup](a)
	x.Label = sa.Intern(optString(grp, _grp_label))
	x.Title = sa.Intern(optString(grp, _grp_title))
	x.ReservationLinks = mapList(a, grp.Get(_grp_reservation_links), func(lnk protoreflect.Value) ReservationLink {
		return makeReservationLink(sa, lnk.Message())
	})
	x.ScheduleChangesHTML = sa.Intern(optString(grp, _grp_schedule_changes_html))
	x.NoResv = optBool(grp, _grp_noresv)
	return x
}

func newSchedule(a *arena, sa *stringInterner, sch protoreflect.Message) *xSchedule {
	x := arenaNew[xSchedule](a)
	x.Caption = sa.Intern(optString(sch, _sch_caption))
	x.Name = sa.Intern(optString(sch, _sch_name))
	x.Date = sa.Intern(optString(sch, _sch_date))
	if from, to := sch.Get(_sch_from), sch.Get(_sch_to); from.IsValid() && to.IsValid() {
		x.DateRange.From = schema.Date(from.Int())
		x.DateRange.To = schema.Date(to.Int())
	} else {
		x.DateRange.From = -1
		x.DateRange.To = -1
	}
	x.Days = mapList(a, sch.Get(_sch_days), func(m protoreflect.Value) string {
		return sa.InternFast(m.String())
	})
	x.DayDates = mapList(a, sch.Get(_sch_daydates), func(m protoreflect.Value) schema.Date {
		return schema.Date(m.Int())
	})
	return x
}

type activityInterner struct {
	a  *arena
	sa *stringInterner
	c  map[string][]*xActivity
	n  int64
}

func (c *activityInterner) newActivity(act protoreflect.Message) *xActivity {
	c.n++
	if c.c == nil {
		c.c = make(map[string][]*xActivity, 512)
	}
	if s, ok := c.c[optString(act, _act_label)]; ok {
		for _, x := range s {
			if x.Label == optString(act, _act_label) &&
				x.Name == optString(act, _act_name) &&
				x.Resv == optBool(act, _act_resv) &&
				x.HasResv == act.Get(_act_resv).IsValid() {
				return x
			}
		}
	}

	x := arenaNew[xActivity](c.a)
	x.Label = c.sa.Intern(optString(act, _act_label))
	x.Name = c.sa.Intern(optString(act, _act_name))
	x.Resv = optBool(act, _act_resv)
	x.HasResv = act.Get(_act_resv).IsValid()

	s, ok := c.c[x.Label]
	if !ok {
		s = make([]*xActivity, 0, 4)
	}
	s = append(s, x)
	c.c[x.Label] = s

	return x
}

type timeInterner struct {
	a  *arena
	sa *stringInterner
	c  map[string][]*xTime
	n  int64
}

func (c *timeInterner) newTime(scheduleDay int, tm protoreflect.Message) *xTime {
	var (
		wkday time.Weekday
		rng   schema.ClockRange
	)
	if s, e, w := tm.Get(_tm_start), tm.Get(_tm_end), tm.Get(_tm_wkday); s.IsValid() && e.IsValid() && w.IsValid() {
		wkday = time.Weekday(w.Enum())
		rng.Start = schema.ClockTime(s.Int())
		rng.End = schema.ClockTime(e.Int())
	} else {
		wkday = -1
		rng = schema.ClockRange{}
	}

	c.n++
	if c.c == nil {
		c.c = make(map[string][]*xTime, 4096)
	}
	if s, ok := c.c[optString(tm, _tm_label)]; ok {
		for _, x := range s {
			if x.ScheduleDay == scheduleDay &&
				x.Label == optString(tm, _tm_label) &&
				x.Weekday == wkday &&
				x.Range == rng {
				return x
			}
		}
	}

	x := arenaNew[xTime](c.a)
	x.ScheduleDay = scheduleDay
	x.Label = c.sa.Intern(optString(tm, _tm_label))
	x.Weekday = wkday
	x.Range = rng

	s, ok := c.c[x.Label]
	if !ok {
		s = make([]*xTime, 0, 4)
	}
	s = append(s, x)
	c.c[x.Label] = s

	return x
}

func makeReservationLink(sa *stringInterner, lnk protoreflect.Message) ReservationLink {
	return ReservationLink{
		Label: sa.InternFast(optString(lnk, _lnk_label)),
		URL:   sa.InternFast(optString(lnk, _lnk_url)),
	}
}

func mapList[T any](a *arena, s protoreflect.Value, fn func(m protoreflect.Value) T) []T {
	if !s.IsValid() {
		return nil
	}
	l := s.List()
	n := l.Len()
	if n == 0 {
		return nil
	}
	x := arenaMakeSlice[T](a, n, n)
	for i := range n {
		x[i] = fn(l.Get(i))
	}
	return x
}

func descriptorOf[T interface {
	ProtoReflect() protoreflect.Message
}]() protoreflect.MessageDescriptor {
	var z T
	return z.ProtoReflect().Descriptor()
}

func iterMessageList(m protoreflect.Value) iter.Seq2[int, protoreflect.Message] {
	return func(yield func(int, protoreflect.Message) bool) {
		if m.IsValid() {
			l := m.List()
			for i := range l.Len() {
				if !yield(i, l.Get(i).Message()) {
					return
				}
			}
		}
	}
}

func mustField(d protoreflect.MessageDescriptor, name protoreflect.Name) protoreflect.FieldDescriptor {
	x := d.Fields().ByName(name)
	if x == nil {
		panic(fmt.Sprintf("schema: type %s missing field %s", d.FullName(), name))
	}
	return x
}

func optString(m protoreflect.Message, f protoreflect.FieldDescriptor) string {
	if f.Kind() != protoreflect.StringKind {
		panic("not a string")
	}
	v := m.Get(f)
	if !v.IsValid() {
		return ""
	}
	return v.String()
}

func optBool(m protoreflect.Message, f protoreflect.FieldDescriptor) bool {
	if f.Kind() != protoreflect.BoolKind {
		panic("not a bool")
	}
	v := m.Get(f)
	if !v.IsValid() {
		return false
	}
	return v.Bool()
}
