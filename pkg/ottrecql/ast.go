package ottrecql

import "time"

// Node is implemented by all AST expression nodes.
// Each node also carries the character offset into the input where it begins.
type Node interface{ exprNode() }

func (*NotNode) exprNode()      {}
func (*AndNode) exprNode()      {}
func (*OrNode) exprNode()       {}
func (*SchDateNode) exprNode()  {}
func (*TimeNode) exprNode()     {}
func (*FacilityNode) exprNode() {}
func (*ActivityNode) exprNode() {}
func (*LatLngNode) exprNode()   {}

// DaySpec is a day argument inside time() ([WeekdayLit] or [DateLit]).
type DaySpec interface{ daySpec() }

func (WeekdayLit) daySpec() {}
func (DateLit) daySpec()    {}

// TimeSpec is a time argument inside time() ([TimeLit] or [TimeRangeLit]).
type TimeSpec interface{ timeSpec() }

func (TimeLit) timeSpec()      {}
func (TimeRangeLit) timeSpec() {}

// NotNode is a logical NOT: not <expr> or !<expr>.
type NotNode struct {
	Pos
	Expr Node
}

// AndNode is a logical AND: <left> and <right> or <left> && <right>.
type AndNode struct {
	Pos
	Left  Node
	Right Node
}

// OrNode is a logical OR: <left> or <right> or <left> || <right>.
type OrNode struct {
	Pos
	Left  Node
	Right Node
}

// DateLit represents a date literal (YYYY-MM-DD or the special value "today").
type DateLit struct {
	Pos
	IsToday bool // if true, everything else is ignored
	Year    int
	Month   time.Month
	Day     int
}

// TimeLit represents a time literal (HH:MM[a|am|p|pm] or the special value "now").
type TimeLit struct {
	Pos
	IsNow     bool // if true, everything else is ignored
	Hour      int
	Minute    int
	PM        bool // true if PM was specified (only meaningful when HasPeriod is true)
	HasPeriod bool // true if an AM/PM suffix was present (12-hour format)
}

// WeekdayLit is a weekday literal.
type WeekdayLit struct {
	Pos
	Weekday time.Weekday
}

// TimeRangeLit represents a time range (start-end).
type TimeRangeLit struct {
	Start TimeLit
	End   TimeLit
}

// SchDateNode represents schdate(date).
type SchDateNode struct {
	Pos
	Date DateLit
}

// TimeNode represents time([weekday...|date...] @ [time...|timerange...]).
type TimeNode struct {
	Pos
	Days  []DaySpec  // OR'd weekdays/dates, empty if omitted
	Times []TimeSpec // OR'd times/ranges, empty if omitted
}

// FacilityNode represents facility([string...]).
type FacilityNode struct {
	Pos
	Strings []string
}

// ActivityNode represents activity([string...]).
type ActivityNode struct {
	Pos
	Strings []string
}

// LatLngNode represents latlng(lat, lng, km).
type LatLngNode struct {
	Pos
	Lat  float32
	Lng  float32
	Dist float32
}
