package templates

import (
	"reflect"
	"testing"
)

func mask(set map[int][]int) [7]byte {
	var m [7]byte
	for wd, slots := range set {
		for _, s := range slots {
			m[wd] |= 1 << s
		}
	}
	return m
}

// weekday indices and the slot layout (mirrors mapSlots) used in expectations:
//
//	0: 00:00-06:00   1: 06:00-09:00   2: 09:00-11:00   3: 11:00-13:00
//	4: 13:00-16:00   5: 16:00-18:00   6: 18:00-21:00   7: 21:00-24:00
const (
	sun = 0
	mon = 1
	fri = 5
	sat = 6
)

func hm(h, m int) int { return h*60 + m }

func TestMaskSetRange(t *testing.T) {
	for _, tt := range []struct {
		name       string
		wd         int
		start, end int
		want       map[int][]int
	}{{
		name:  "within a single slot",
		wd:    mon,
		start: hm(9, 30), end: hm(10, 0),
		want: map[int][]int{mon: {2}},
	}, {
		name:  "spans several slots",
		wd:    mon,
		start: hm(8, 0), end: hm(12, 0),
		want: map[int][]int{mon: {1, 2, 3}},
	}, {
		name:  "exact slot boundaries don't bleed into neighbours",
		wd:    mon,
		start: hm(6, 0), end: hm(9, 0),
		want: map[int][]int{mon: {1}},
	}, {
		name:  "touching a boundary only (start == slot end) doesn't set it",
		wd:    mon,
		start: hm(6, 0), end: hm(6, 0),
		want: map[int][]int{},
	}, {
		name:  "full day sets every slot",
		wd:    mon,
		start: hm(0, 0), end: hm(24, 0),
		want: map[int][]int{mon: {0, 1, 2, 3, 4, 5, 6, 7}},
	}, {
		name:  "ends exactly at midnight, no wrap",
		wd:    mon,
		start: hm(21, 0), end: hm(24, 0),
		want: map[int][]int{mon: {7}},
	}, {
		name:  "overnight range wraps onto the next day",
		wd:    fri,
		start: hm(22, 0), end: hm(25, 0),
		want: map[int][]int{fri: {7}, sat: {0}},
	}, {
		name:  "overnight wrap crosses the week boundary (Sat -> Sun)",
		wd:    sat,
		start: hm(23, 0), end: hm(24, 30),
		want: map[int][]int{sat: {7}, sun: {0}},
	}, {
		name:  "start past midnight shifts onto the next day",
		wd:    sun,
		start: hm(25, 0), end: hm(26, 0),
		want: map[int][]int{mon: {0}},
	}, {
		name:  "start exactly at 24h shifts onto the next day",
		wd:    sun,
		start: hm(24, 0), end: hm(26, 0),
		want: map[int][]int{mon: {0}},
	}} {
		t.Run(tt.name, func(t *testing.T) {
			var m [7]byte
			maskSetRange(&m, mapSlots, tt.wd, tt.start, tt.end)
			if want := mask(tt.want); m != want {
				t.Errorf("maskSetRange(wd=%d, %d, %d) = %v, want %v", tt.wd, tt.start, tt.end, m, want)
			}
		})
	}
}

func TestMaskSetRangeAccumulates(t *testing.T) {
	var m [7]byte
	maskSetRange(&m, mapSlots, mon, hm(9, 0), hm(11, 0))  // slot 2
	maskSetRange(&m, mapSlots, mon, hm(18, 0), hm(21, 0)) // slot 6
	maskSetRange(&m, mapSlots, fri, hm(13, 0), hm(16, 0)) // slot 4
	if want := mask(map[int][]int{mon: {2, 6}, fri: {4}}); !reflect.DeepEqual(m, want) {
		t.Errorf("accumulated mask = %v, want %v", m, want)
	}
}
