package ottrecidx

import (
	"iter"
	"math"
	"slices"
	"strconv"
	"time"
	"unsafe"

	"github.com/pgaskin/ottrec/schema"
)

// this file contains the user-facing interface to the indexed data

// we use getters and iterators to ensure stuff can't be directly mutated by
// accident, and to give us more flexibility about the underlying implementation
// for future optimization

// a filter must never mask out parent schema objects without also masking out
// the children (this helps keep the logic easy to reason about and reduces the
// chance of having subtle bugs which don't panic)

// in general, it should not be possible for a user of the package to obtain an
// invalid ref using the provided APIs except where it's the result of trying to
// find a non-existent obj or something similar

// all of these nested structs and generics seems complicated, but it's much
// less error prone as we can enforce almost everything except for parent-child
// relationships though the type system.

// refObj is an index into the objects array, or a high value for special
// objects not directly part of the schedule tree.
type refObj uint32

const (
	refObjTest refObj = math.MaxUint32 - iota // not actually used, just a placeholder for if we need this in the future
	refObjSpecialMin
)

func (obj refObj) String() string {
	switch obj {
	case refObjTest:
		return "TEST"
	}
	if obj > refObjSpecialMin {
		panic("wtf") // missing case
	}
	return strconv.FormatUint(uint64(obj), 10)
}

func (obj refObj) isSpecial() bool {
	return obj > refObjSpecialMin
}

// anyRef is an interface implemented by all levels of abstraction of a reference.
type anyRef interface {
	reflect() baseRef
}

// baseRef contains the underlying structure of a reference.
type baseRef struct {
	idx *Index
	flt bitmap[refObj]
	obj refObj
}

func (ref baseRef) reflect() baseRef {
	return ref
}

// RefID is an opaque comparable value which efficiently uniquely identifies the
// ref at runtime without keeping a reference to it.
type RefID struct {
	id uint64
}

func (ref baseRef) RefID() RefID {
	if ref.idx == nil {
		return RefID{}
	}
	return RefID{ref.idx.hashCode ^ uint64(ref.obj)}
}

// Valid returns true if the ref is not a nil reference. If the ref is not
// valid, this (and String/GoString) are the only functions which can be safely
// called without panicking.
func (ref baseRef) Valid() bool {
	return ref.idx != nil
}

// index gets the underlying index the ref came from.
func (ref baseRef) index() *Index {
	if !ref.Valid() {
		panic("cannot get index of nil reference")
	}
	return ref.idx
}

// object gets the object the ref points to.
func (ref baseRef) object() refObj {
	if !ref.Valid() {
		panic("cannot get object of nil reference")
	}
	return ref.obj
}

// withFilter returns a copy of ref with a clone of the withFilter, or a new
// filter including everything.
func (ref baseRef) withFilter() baseRef {
	if ref.flt.IsNil() {
		ref.flt = makeBitmap[refObj](len(ref.index().obj))
		ref.flt.Ones()
	} else {
		ref.flt = ref.flt.Clone()
	}
	return ref
}

// deref returns the schema object the ref points to.
func (ref baseRef) deref() objRef {
	if !ref.Valid() {
		panic("cannot deref nil reference")
	}
	if ref.obj.isSpecial() {
		switch ref.obj {
		//case refObjTest:
		//	return ref.idx.test
		}
		panic("wtf: missing special case in deref")
	}
	if !ref.flt.IsNil() && !ref.flt.Contains(ref.obj) {
		// at first, you might think this shouldn't be a panic, and should
		// be a supported condition, but consider that if we didn't have all
		// this ref stuff, it'd essentially be like trying to access a
		// deleted item
		panic("invalid ref: references filtered obj") // maybe someone accidentally mutated the filter in-place, or maybe they forgot to apply the filter?
	}
	return ref.idx.obj[ref.obj]
}

// typedRef extends [baseRef] with type-safe helpers and additional checks.
type typedRef[T schemaObj] struct {
	baseRef
	_ [0]*T // prevent direct conversions between typedRef types
}

// typeBitmap returns the index bitmap for objects of the specified type, or a nil
// bitmap if it's a special object.
func typeBitmap[T schemaObj](idx *Index) bitmap[refObj] {
	switch any((*T)(nil)).(type) {
	case *xData:
		return idx.bData
	case *xFacility:
		return idx.bFacility
	case *xScheduleGroup:
		return idx.bScheduleGroup
	case *xSchedule:
		return idx.bSchedule
	case *xActivity:
		return idx.bActivity
	case *xTime:
		return idx.bTime
	}
	return nilBitmap[refObj]()
}

// typeNotChildBitmap returns the bitmap of all objects at or above the
// specified type..
func typeNotChildBitmap[T schemaObj](idx *Index) bitmap[refObj] {
	switch any((*T)(nil)).(type) {
	case *xData:
		return idx.bDataNotChild
	case *xFacility:
		return idx.bFacilityNotChild
	case *xScheduleGroup:
		return idx.bScheduleGroupNotChild
	case *xSchedule:
		return idx.bScheduleNotChild
	case *xActivity:
		return idx.bActivityNotChild
	case *xTime:
		return idx.bTimeNotChild
	}
	return nilBitmap[refObj]()
}

// reference checks and creates a reference from an existing reference.
func reference[T schemaObj](ref anyRef, obj refObj) typedRef[T] {
	oref := ref.reflect()
	_ = oref.deref()
	nref := typedRef[T]{baseRef: baseRef{oref.idx, oref.flt, obj}}
	_ = nref.deref()
	return nref
}

// objRef is a type which holds a pointer to a [schemaObj] referenced by a
// [refObj].
type objRef = unsafe.Pointer // to have Go type-checking, use = any, but that significantly increases the memory overhead per indexed dataset

// deref returns the schema object the ref points to.
func (ref typedRef[T]) deref() *T {
	v := ref.baseRef.deref()
	if !ref.obj.isSpecial() && !ref.typeBitmap().Contains(ref.obj) {
		panic("invalid ref: obj type doesn't match typedRef type")
	}
	/*
		x, ok := v.(*T)
		if !ok {
			panic("wtf: inconsistent index bitmap or baseRef.deref implementation")
		}
		return x
	*/
	return (*T)(v) // safe as long as the index bitmaps aren't buggy
}

// typeBitmap returns the bitmap of all other objects of the current type.
func (ref typedRef[T]) typeBitmap() bitmap[refObj] {
	return typeBitmap[T](ref.index())
}

// typeNotChildBitmap returns the bitmap of all objects at or above the
// current type..
func (ref typedRef[T]) typeNotChildBitmap() bitmap[refObj] {
	return typeNotChildBitmap[T](ref.index())
}

// withFilter returns a copy of ref with a clone of the filter, or a new filter
// including everything.
func (ref typedRef[T]) withFilter() typedRef[T] {
	return typedRef[T]{baseRef: ref.baseRef.withFilter()}
}

// nthOfType returns the index of ref from all objects of the same type,
// starting from zero. If ref is a special type, -1 is returned.
func (ref typedRef[T]) nthOfType() int {
	bm := ref.typeBitmap()
	if bm.IsNil() {
		return -1
	}
	return bm.CountTo(ref.object())
}

// Boxed typedRefs with exposed getters, setters, and iterators.
type (
	DataRef          struct{ typedRef[xData] }
	FacilityRef      struct{ typedRef[xFacility] }
	ScheduleGroupRef struct{ typedRef[xScheduleGroup] }
	ScheduleRef      struct{ typedRef[xSchedule] }
	ActivityRef      struct{ typedRef[xActivity] }
	TimeRef          struct{ typedRef[xTime] }
)

func (ref DataRef) Index() *Index          { return ref.index() }
func (ref FacilityRef) Index() *Index      { return ref.index() }
func (ref ScheduleGroupRef) Index() *Index { return ref.index() }
func (ref ScheduleRef) Index() *Index      { return ref.index() }
func (ref ActivityRef) Index() *Index      { return ref.index() }
func (ref TimeRef) Index() *Index          { return ref.index() }

// GetAttribution returns the data attribution strings.
func (ref DataRef) GetAttribution() iter.Seq[string] { return slices.Values(ref.deref().Attribution) }

// GetName returns the facility name.
func (ref FacilityRef) GetName() string { return ref.deref().Name }

// GetSourceURL returns the source URL for the facility.
func (ref FacilityRef) GetSourceURL() string { return ref.deref().SourceURL }

// GetSourceDate returns the date the facility data was sourced.
func (ref FacilityRef) GetSourceDate() time.Time { return ref.deref().SourceDate }

// GetAddress returns the facility address.
func (ref FacilityRef) GetAddress() string { return ref.deref().Address }

// GetLngLat returns the longitude and latitude of the facility, parsed by the
// scraper on a best-effort basis. If no coordinates are available, ok is false.
func (ref FacilityRef) GetLngLat() (lng float32, lat float32, ok bool) {
	x := ref.deref()
	lng, lat = x.Longitude, x.Latitude
	ok = lng != 0 || lat != 0
	return
}

// GetNotificationsHTML returns the raw HTML for facility notifications.
func (ref FacilityRef) GetNotificationsHTML() string { return ref.deref().NotificationsHTML }

// GetSpecialHoursHTML returns the raw HTML for facility special hours.
func (ref FacilityRef) GetSpecialHoursHTML() string { return ref.deref().SpecialHoursHTML }

// GetErrors returns scrape errors for the facility (e.g., failed time parsing).
func (ref FacilityRef) GetErrors() iter.Seq[string] { return slices.Values(ref.deref().Errors) }

// GetLabel returns the schedule group label.
func (ref ScheduleGroupRef) GetLabel() string { return ref.deref().Label }

// GetTitle returns the schedule group title, parsed from the label and
// normalized during scraping, and in title case, for display and filtering.
func (ref ScheduleGroupRef) GetTitle() string { return ref.deref().Title }

// GetReservationLinks returns the reservation links for the schedule group.
func (ref ScheduleGroupRef) GetReservationLinks() iter.Seq[ReservationLink] {
	return slices.Values(ref.deref().ReservationLinks)
}

// GetScheduleChangesHTML returns the raw HTML for schedule changes.
func (ref ScheduleGroupRef) GetScheduleChangesHTML() string { return ref.deref().ScheduleChangesHTML }

// GetNoResv reports whether top-level text explicitly states that reservations
// are not required. See also [ActivityRef.GetResv].
//
// You probably want to use [ActivityRef.GuessReservationRequirement] instead.
func (ref ScheduleGroupRef) GetNoResv() bool { return ref.deref().NoResv }

// GetCaption returns the schedule caption.
func (ref ScheduleRef) GetCaption() string { return ref.deref().Caption }

// GetName returns the schedule name, parsed from the caption and normalized
// without the facility name or date range during scraping, in lowercase, for
// filtering.
func (ref ScheduleRef) GetName() string { return ref.deref().Name }

// GetDate returns the raw date range string extracted from the caption during
// scraping. Empty if no date-like string can be found in the caption.
func (ref ScheduleRef) GetDate() string { return ref.deref().Date }

// GetDateRange returns the inclusive from and to dates in YYYYMMDDW format. ok
// is false if not set, a parse error occurred, or the date was ambiguous. This
// is parsed from the date in the caption during scraping.
func (ref ScheduleRef) GetDateRange() (schema.DateRange, bool) {
	v := ref.deref().DateRange
	return v, v.From != 0 || v.To != 0
}

// NumDays returns the number of days in the schedule.
func (ref ScheduleRef) NumDays() int { return len(ref.deref().Days) }

// GetDay returns the free-form day label at index i, usually the day of the
// week.
func (ref ScheduleRef) GetDay(i int) string { return ref.deref().Days[i] }

// GetDayDate returns the best-effort parsed date (YYYYMMDDW format) for day i.
// ok is false if the date could not be parsed unambiguously. This is parsed
// from the day label during scraping.
func (ref ScheduleRef) GetDayDate(i int) (schema.Date, bool) {
	v := ref.deref().DayDates
	if i >= len(v) {
		return 0, false
	}
	d := v[i]
	return d, d != 0
}

// GetLabel returns the activity label.
func (ref ActivityRef) GetLabel() string { return ref.deref().Label }

// GetName returns the activity name, cleaned up, normalized, and lowercased
// during scraping, for filtering.
//
// Activity verbs are generally normalized to the infinitive form for recognized
// activities (e.g., skate rather than skating, swim rather than swimming).
func (ref ActivityRef) GetName() string { return ref.deref().Name }

// GetResv returns the reservation requirement for the activity. hasResv is
// false if no explicit reservation requirement is stated; otherwise resv
// indicates whether reservations are required.
//
// You probably want to use [ActivityRef.GuessReservationRequirement] instead.
func (ref ActivityRef) GetResv() (resv bool, hasResv bool) {
	v := ref.deref()
	return v.Resv, v.HasResv
}

// GetScheduleDayIndex returns the index into the parent schedule's days for
// this time entry.
func (ref TimeRef) GetScheduleDayIndex() int { return ref.deref().ScheduleDay }

// GetLabel returns the time range label.
func (ref TimeRef) GetLabel() string { return ref.deref().Label }

// GetWeekday returns the weekday parsed from the time range label during
// scraping, where Sunday = 0. If parsing failed, ok is false.
func (ref TimeRef) GetWeekday() (time.Weekday, bool) {
	v := ref.deref().Weekday
	return v, v != -1
}

// GetRange returns the clock range parsed from the time range label during
// scraping, in minutes from 00:00. If parsing failed, ok is false.
func (ref TimeRef) GetRange() (schema.ClockRange, bool) {
	v := ref.deref().Range
	return v, v.Start != 0 || v.End != 0
}

// parentRef returns a ref to the parent of the specified object. It assumes
// that T is a child of U, and will silently misbehave if it isn't.
func parentRef[T, U schemaObj](ref typedRef[T]) typedRef[U] {
	if bm := typeBitmap[U](ref.index()); !bm.IsNil() {
		return reference[U](ref, mustOK(bm.Prev(ref.object())))
	}
	panic("cannot get parent reference of special object")
}

// Data returns the direct parent data.
func (ref FacilityRef) Data() DataRef {
	return DataRef{parentRef[xFacility, xData](ref.typedRef)}
}

// Data returns the ancestor data (skipping over the facility).
func (ref ScheduleGroupRef) Data() DataRef {
	return DataRef{parentRef[xScheduleGroup, xData](ref.typedRef)}
}

// Facility returns the direct parent facility.
func (ref ScheduleGroupRef) Facility() FacilityRef {
	return FacilityRef{parentRef[xScheduleGroup, xFacility](ref.typedRef)}
}

// Data returns the ancestor data (skipping over the facility and schedule
// group).
func (ref ScheduleRef) Data() DataRef {
	return DataRef{parentRef[xSchedule, xData](ref.typedRef)}
}

// Facility returns the ancestor facility (skipping over the schedule group).
func (ref ScheduleRef) Facility() FacilityRef {
	return FacilityRef{parentRef[xSchedule, xFacility](ref.typedRef)}
}

// ScheduleGroup returns the direct parent schedule group.
func (ref ScheduleRef) ScheduleGroup() ScheduleGroupRef {
	return ScheduleGroupRef{parentRef[xSchedule, xScheduleGroup](ref.typedRef)}
}

// Data returns the ancestor data (skipping over the facility, schedule group,
// and schedule).
func (ref ActivityRef) Data() DataRef {
	return DataRef{parentRef[xActivity, xData](ref.typedRef)}
}

// Facility returns the ancestor facility (skipping over the schedule group and
// schedule).
func (ref ActivityRef) Facility() FacilityRef {
	return FacilityRef{parentRef[xActivity, xFacility](ref.typedRef)}
}

// ScheduleGroup returns the ancestor schedule group (skipping over the
// schedule).
func (ref ActivityRef) ScheduleGroup() ScheduleGroupRef {
	return ScheduleGroupRef{parentRef[xActivity, xScheduleGroup](ref.typedRef)}
}

// Schedule returns the direct parent schedule.
func (ref ActivityRef) Schedule() ScheduleRef {
	return ScheduleRef{parentRef[xActivity, xSchedule](ref.typedRef)}
}

// Data returns the ancestor data (skipping over the facility, schedule group,
// schedule, and activity).
func (ref TimeRef) Data() DataRef {
	return DataRef{parentRef[xTime, xData](ref.typedRef)}
}

// Facility returns the ancestor facility (skipping over the schedule group,
// schedule, and activity).
func (ref TimeRef) Facility() FacilityRef {
	return FacilityRef{parentRef[xTime, xFacility](ref.typedRef)}
}

// ScheduleGroup returns the ancestor schedule group (skipping over the
// schedule and activity).
func (ref TimeRef) ScheduleGroup() ScheduleGroupRef {
	return ScheduleGroupRef{parentRef[xTime, xScheduleGroup](ref.typedRef)}
}

// Schedule returns the ancestor schedule (skipping over the activity).
func (ref TimeRef) Schedule() ScheduleRef {
	return ScheduleRef{parentRef[xTime, xSchedule](ref.typedRef)}
}

// Activity returns the direct parent activity.
func (ref TimeRef) Activity() ActivityRef {
	return ActivityRef{parentRef[xTime, xActivity](ref.typedRef)}
}

// childRefSeq yields filtered references for objects of type U up to the next
// T.
func childRefSeq[T, U schemaObj](ref typedRef[T]) iter.Seq[typedRef[U]] {
	return func(yield func(typedRef[U]) bool) {
		// check and start at ref
		start := ref.object()
		if start.isSpecial() {
			panic("wtf: T is a special object")
		}
		// find the end of ref's children, otherwise the reset of the objects
		var until refObj
		if next, ok := ref.typeNotChildBitmap().Next(start + 1); ok {
			until = next // next sibling or a different parent
		} else {
			until = refObj(len(ref.idx.obj)) // end
		}
		if mask := typeBitmap[U](ref.index()); !mask.IsNil() {
			for obj := range mask.RangeBetweenAnd(start, until, ref.flt) {
				if !yield(reference[U](ref, obj)) {
					return
				}
			}
		} else {
			panic("wtf: U is a special object")
		}
	}
}

// Facilities returns the direct child facilities.
func (ref DataRef) Facilities() FacilitySeq {
	return facilitySeq(childRefSeq[xData, xFacility](ref.typedRef))
}

// ScheduleGroups returns all schedule groups nested within this data
// (across all facilities).
func (ref DataRef) ScheduleGroups() ScheduleGroupSeq {
	return scheduleGroupSeq(childRefSeq[xData, xScheduleGroup](ref.typedRef))
}

// Schedules returns all schedules nested within this data (across all
// facilities and schedule groups).
func (ref DataRef) Schedules() ScheduleSeq {
	return scheduleSeq(childRefSeq[xData, xSchedule](ref.typedRef))
}

// Activities returns all activities nested within this data (across all
// facilities, schedule groups, and schedules).
func (ref DataRef) Activities() ActivitySeq {
	return activitySeq(childRefSeq[xData, xActivity](ref.typedRef))
}

// Times returns all time entries nested within this data (across all
// facilities, schedule groups, schedules, and activities).
func (ref DataRef) Times() TimeSeq {
	return timeSeq(childRefSeq[xData, xTime](ref.typedRef))
}

// ScheduleGroups returns the direct child schedule groups.
func (ref FacilityRef) ScheduleGroups() ScheduleGroupSeq {
	return scheduleGroupSeq(childRefSeq[xFacility, xScheduleGroup](ref.typedRef))
}

// Schedules returns all schedules nested within this facility (across all
// schedule groups).
func (ref FacilityRef) Schedules() ScheduleSeq {
	return scheduleSeq(childRefSeq[xFacility, xSchedule](ref.typedRef))
}

// Activities returns all activities nested within this facility (across all
// schedule groups and schedules).
func (ref FacilityRef) Activities() ActivitySeq {
	return activitySeq(childRefSeq[xFacility, xActivity](ref.typedRef))
}

// Times returns all time entries nested within this facility (across all
// schedule groups, schedules, and activities).
func (ref FacilityRef) Times() TimeSeq {
	return timeSeq(childRefSeq[xFacility, xTime](ref.typedRef))
}

// Schedules returns the direct child schedules.
func (ref ScheduleGroupRef) Schedules() ScheduleSeq {
	return scheduleSeq(childRefSeq[xScheduleGroup, xSchedule](ref.typedRef))
}

// Activities returns all activities nested within this schedule group (across
// all schedules).
func (ref ScheduleGroupRef) Activities() ActivitySeq {
	return activitySeq(childRefSeq[xScheduleGroup, xActivity](ref.typedRef))
}

// Times returns all time entries nested within this schedule group (across all
// schedules and activities).
func (ref ScheduleGroupRef) Times() TimeSeq {
	return timeSeq(childRefSeq[xScheduleGroup, xTime](ref.typedRef))
}

// Activities returns the direct child activities.
func (ref ScheduleRef) Activities() ActivitySeq {
	return activitySeq(childRefSeq[xSchedule, xActivity](ref.typedRef))
}

// Times returns all time entries nested within this schedule (across all
// activities).
func (ref ScheduleRef) Times() TimeSeq {
	return timeSeq(childRefSeq[xSchedule, xTime](ref.typedRef))
}

// Times returns the direct child time entries.
func (ref ActivityRef) Times() TimeSeq {
	return timeSeq(childRefSeq[xActivity, xTime](ref.typedRef))
}

// GetScheduleDay calls [ScheduleRef.GetDay] for the schedule day corresponding
// to this [TimeRef].
func (ref TimeRef) GetScheduleDay() string {
	return ref.Schedule().GetDay(ref.deref().ScheduleDay)
}

// GetScheduleDay calls [ScheduleRef.GetDayDate] for the schedule day
// corresponding to this [TimeRef].
func (ref TimeRef) GetScheduleDayDate() (schema.Date, bool) {
	return ref.Schedule().GetDayDate(ref.deref().ScheduleDay)
}

// DayTimes returns the direct child time entries for schedule day index i.
func (ref ActivityRef) DayTimes(i int) TimeSeq {
	return TimeSeq(func(yield func(TimeRef) bool) {
		for tm := range ref.Times() {
			if tm.GetScheduleDayIndex() == i {
				if !yield(tm) {
					return
				}
			}
		}
	})
}

// TODO: more helpers

func mustOK[T any](x T, ok bool) T {
	if !ok {
		panic("wtf")
	}
	return x
}
