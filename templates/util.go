package templates

import (
	"iter"
	"strings"

	"github.com/pgaskin/ottrec/schema"
)

func cutBefore(s, sep string) string {
	before, _, _ := strings.Cut(s, sep)
	return before
}

func iterEnumerate[T any](seq iter.Seq[T]) iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		var i int
		for x := range seq {
			if !yield(i, x) {
				return
			}
			i++
		}
	}
}

func iterEmpty[T any](seq iter.Seq[T]) bool {
	for range seq {
		return false
	}
	return true
}

func prettyTimeRange(r schema.ClockRange) string {
	if !r.IsValid() {
		return "invalid"
	}
	prettyTime := func(t schema.ClockTime) string {
		if t == 12*60 {
			return "noon"
		}
		if t == 0 || t == 24*60 {
			return "midnight"
		}
		var b strings.Builder
		_, hh, mm := t.Split()
		ap := byte('a')
		if hh >= 12 {
			ap = 'p'
			hh -= 12
		}
		if hh == 0 {
			b.WriteByte('1')
			b.WriteByte('2')
		} else {
			if hh >= 10 {
				b.WriteByte('0' + byte(hh/10))
			}
			b.WriteByte('0' + byte(hh%10))
		}
		if mm != 0 {
			b.WriteByte(':')
			b.WriteByte('0' + byte(mm/10))
			b.WriteByte('0' + byte(mm%10))
		}
		b.WriteByte(' ')
		b.WriteByte(ap)
		b.WriteByte('m')
		return b.String()
	}
	x := prettyTime(r.Start)
	y := prettyTime(r.End)
	if x[len(x)-2] == y[len(y)-2] {
		x = x[:len(x)-3]
	}
	return x + " - " + y
}
