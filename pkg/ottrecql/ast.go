package ottrecql

import "time"

// Date represents a date literal (YYYY-MM-DD or the special value "today").
type Date struct {
	Offset  int
	IsToday bool // if true, everything else is ignored
	Year    int
	Month   time.Month
	Day     int
}

// Time represents a time literal (HH:MM[a|am|p|pm] or the special value "now").
type Time struct {
	Offset    int
	IsNow     bool // if true, everything else is ignored
	Hour      int
	Minute    int
	PM        bool // true if PM was specified (only meaningful when HasPeriod is true)
	HasPeriod bool // true if an AM/PM suffix was present (12-hour format)
}

// TimeRange represents a time range (start-end).
type TimeRange struct {
	Start Time
	End   Time
}

// Expr is implemented by all AST expression nodes.
// Each node also carries the character offset into the input where it begins.
type Expr interface {
	exprNode()
}

// NotExpr is a logical NOT: not <expr> or !<expr>.
type NotExpr struct {
	Offset int
	Expr   Expr
}

func (*NotExpr) exprNode() {}

// AndExpr is a logical AND: <left> and <right> or <left> && <right>.
type AndExpr struct {
	Offset int
	Left   Expr
	Right  Expr
}

func (*AndExpr) exprNode() {}

// OrExpr is a logical OR: <left> or <right> or <left> || <right>.
type OrExpr struct {
	Offset int
	Left   Expr
	Right  Expr
}

func (*OrExpr) exprNode() {}

// SchDateExpr represents schdate(date).
type SchDateExpr struct {
	Offset int
	Date   Date
}

func (*SchDateExpr) exprNode() {}

// DaySpec is a day argument inside time(): either a Weekday or a Date.
type DaySpec interface {
	daySpec()
}

// WeekdaySpec is a weekday argument in time().
type WeekdaySpec struct {
	Offset  int
	Weekday time.Weekday
}

func (WeekdaySpec) daySpec() {}

// DateSpec is a date argument in time().
type DateSpec struct {
	Date Date
}

func (DateSpec) daySpec() {}

// TimeSpec is a time argument inside time(): either a single Time or a TimeRange.
type TimeSpec interface {
	timeSpec()
}

func (Time) timeSpec()      {}
func (TimeRange) timeSpec() {}

// TimeExpr represents time([weekday...|date...] @ [time...|timerange...]).
type TimeExpr struct {
	Offset int
	Days   []DaySpec  // OR'd weekdays/dates, empty if omitted
	Times  []TimeSpec // OR'd times/ranges, empty if omitted
}

func (*TimeExpr) exprNode() {}

// FacilityExpr represents facility([string...]).
type FacilityExpr struct {
	Offset  int
	Strings []string
}

func (*FacilityExpr) exprNode() {}

// ActivityExpr represents activity([string...]).
type ActivityExpr struct {
	Offset  int
	Strings []string
}

func (*ActivityExpr) exprNode() {}

// LatLngExpr represents latlng(lat, lng, km).
type LatLngExpr struct {
	Offset     int
	Lat        float32
	Lng        float32
	Kilometers float32
}

func (*LatLngExpr) exprNode() {}
