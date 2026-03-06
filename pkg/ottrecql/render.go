package ottrecql

import (
	"strconv"
	"strings"
)

// Render converts a expression node back to its normalized plain-text
// representation with minimal parenthesis.
func Render(e Expr) string {
	return renderExpr(e, 0)
}

// renderExpr renders e, wrapping in parentheses if its binding power is less
// than parentBP (i.e. the context requires tighter binding).
func renderExpr(e Expr, parentBP int) string {
	switch e := e.(type) {
	case *NotExpr:
		// NOT has higher precedence than AND/OR; recurse at bpAnd so that an OR
		// or AND operand gets parenthesised: not (a or b).
		return "not " + renderExpr(e.Expr, bpAnd)
	case *AndExpr:
		//	- left: no parens at same precedence (left-associative)
		//	- right: parens at same precedence to preserve right grouping
		s := renderExpr(e.Left, bpAnd) + " and " + renderExpr(e.Right, bpAnd+1)
		if parentBP > bpAnd {
			return "(" + s + ")"
		}
		return s
	case *OrExpr:
		s := renderExpr(e.Left, bpOr) + " or " + renderExpr(e.Right, bpOr+1)
		if parentBP > bpOr {
			return "(" + s + ")"
		}
		return s
	case *SchDateExpr:
		return "schdate(" + renderDate(e.Date) + ")"
	case *TimeExpr:
		return renderTimeExpr(e)
	case *FacilityExpr:
		return "facility(" + renderStringList(e.Strings) + ")"
	case *ActivityExpr:
		return "activity(" + renderStringList(e.Strings) + ")"
	case *LatLngExpr:
		return "latlng(" + renderFloat(e.Lat) + ", " + renderFloat(e.Lng) + ", " + renderFloat(e.Kilometers) + ")"
	default:
		return "invalid expression"
	}
}

func renderDate(d Date) string {
	if d.IsToday {
		return "today"
	}
	return pad(d.Year, 4) + "-" + pad(int(d.Month), 2) + "-" + pad(d.Day, 2)
}

func renderTime(t Time) string {
	if t.IsNow {
		return "now"
	}
	if !t.HasPeriod {
		return pad(t.Hour, 2) + ":" + pad(t.Minute, 2)
	}
	s := strconv.Itoa(t.Hour) + ":" + pad(t.Minute, 2)
	if t.PM {
		s += "pm"
	} else {
		s += "am"
	}
	return s
}

func renderTimeSpec(ts TimeSpec) string {
	switch v := ts.(type) {
	case Time:
		return renderTime(v)
	case TimeRange:
		return renderTime(v.Start) + "-" + renderTime(v.End)
	}
	return ""
}

func renderTimeExpr(e *TimeExpr) string {
	var b strings.Builder
	b.WriteString("time(")
	for i, d := range e.Days {
		if i > 0 {
			b.WriteString(", ")
		}
		switch v := d.(type) {
		case WeekdaySpec:
			b.WriteString(v.Weekday.String())
		case DateSpec:
			b.WriteString(renderDate(v.Date))
		}
	}
	if len(e.Days) > 0 && len(e.Times) > 0 {
		b.WriteString(" @ ")
	}
	for i, ts := range e.Times {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(renderTimeSpec(ts))
	}
	b.WriteByte(')')
	return b.String()
}

func renderStringList(ss []string) string {
	var b strings.Builder
	for i, s := range ss {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(strconv.Quote(s))
	}
	return b.String()
}

func renderFloat(f float32) string {
	return strconv.FormatFloat(float64(f), 'g', -1, 32)
}

func pad(n, pad int) string {
	s := strconv.Itoa(n)
	for len(s) < pad {
		s = "0" + s
	}
	return s
}
