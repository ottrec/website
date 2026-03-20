package ottrecidx

import (
	"slices"
	"testing"
)

func bitmapOf(bits ...uint32) bitmap[uint32] {
	var b bitmap[uint32]
	for _, i := range bits {
		b.Set(i)
	}
	return b
}

func TestBitmap(t *testing.T) {
	t.Run("IsNil", func(t *testing.T) {
		var zero bitmap[uint32]
		if !zero.IsNil() {
			t.Error("nilBitmap should be nil for zero bitmap")
		}
		if !nilBitmap[uint32]().IsNil() {
			t.Error("nilBitmap should be nil")
		}
		if bitmapOf(1).IsNil() {
			t.Error("non-nil bitmap should not be nil")
		}
	})

	t.Run("Remove", func(t *testing.T) {
		b := bitmapOf(0, 63, 64, 200)
		b.Remove(63)
		if b.Contains(63) {
			t.Error("should have 63")
		}
		if !b.Contains(64) {
			t.Error("should not have 64")
		}
	})

	t.Run("Count", func(t *testing.T) {
		for _, tc := range [][]uint32{
			{},
			{0},
			{1},
			{1, 63},
			{1, 64},
			{1, 65},
			{0, 1, 63, 64, 128},
		} {
			if n := bitmapOf(tc...).Count(); n != len(tc) {
				t.Errorf("bitmapOf(%d).Count() = %d, want %d", tc, n, len(tc))
			}
		}
	})

	t.Run("CountTo", func(t *testing.T) {
		b := bitmapOf(0, 1, 63, 64, 128)
		for _, tc := range []struct {
			until uint32
			want  int
		}{
			{0, 0},
			{1, 1},
			{63, 2},
			{64, 3},
			{128, 4},
			{200, 5},
		} {
			if got := b.CountTo(tc.until); got != tc.want {
				t.Errorf("CountTo(%d) = %d, want %d", tc.until, got, tc.want)
			}
		}
	})

	t.Run("Min", func(t *testing.T) {
		var zero bitmap[uint32]
		if _, ok := zero.Min(); ok {
			t.Error("min of empty bitmap should be false")
		}
		if v, ok := bitmapOf(5, 63, 64, 200).Min(); !ok || v != 5 {
			t.Errorf("Min() = (%d, %v), want (5, true)", v, ok)
		}
	})

	t.Run("Max", func(t *testing.T) {
		var zero bitmap[uint32]
		if _, ok := zero.Max(); ok {
			t.Error("max of empty bitmap should be false")
		}
		if v, ok := bitmapOf(5, 63, 64, 200).Max(); !ok || v != 200 {
			t.Errorf("Max() = (%d, %v), want (200, true)", v, ok)
		}
	})

	t.Run("Or", func(t *testing.T) {
		b := bitmapOf(0, 64)
		b.Or(bitmapOf(1, 64, 128))
		if exp, act := []uint32{0, 1, 64, 128}, slices.Collect(b.Range()); !slices.Equal(exp, act) {
			t.Errorf("expected %v, got %v", exp, act)
		}
	})

	t.Run("And", func(t *testing.T) {
		b := bitmapOf(0, 1, 64, 128)
		b.And(bitmapOf(1, 64, 200))
		if exp, act := []uint32{1, 64}, slices.Collect(b.Range()); !slices.Equal(exp, act) {
			t.Errorf("expected %v, got %v", exp, act)
		}
	})

	t.Run("Ones", func(t *testing.T) {
		b := makeBitmap[uint32](4)
		b.Ones()
		// every allocated bit should be set (note that ones after could also be too)
		for i := range uint32(4) {
			if !b.Contains(i) {
				t.Errorf("missing %d", i)
			}
		}
	})

	t.Run("Clone", func(t *testing.T) {
		b1 := bitmapOf(3, 64, 200)
		b2 := b1.Clone()
		if !slices.Equal(slices.Collect(b1.Range()), slices.Collect(b2.Range())) {
			t.Errorf("bad clone")
		}
		if b1.Remove(3); !b2.Contains(3) {
			t.Errorf("clone is not a clone")
		}
	})

	t.Run("Range", func(t *testing.T) {
		var zero bitmap[uint32]
		if act := slices.Collect(zero.Range()); len(act) != 0 {
			t.Errorf("range of empty bitmap should be empty, got %v", act)
		}
		for _, exp := range [][]uint32{
			{0, 1, 2, 3, 4},
			{0, 63, 64, 65},
		} {
			if act := slices.Collect(bitmapOf(exp...).Range()); !slices.Equal(act, exp) {
				t.Errorf("bad range: expected %v, got %v", exp, act)
			}
		}
		t.Run("Chunks", func(t *testing.T) {
			// stuff is done in 4-bit chunks, so exhaustively verify the possibilities up to 2.5 chunks
			for i := range uint64(0b11_1111_1111) {
				b1 := makeBitmap[uint32](64)
				b1.kb[0] = i
				b2 := makeBitmap[uint32](64)
				for v := range b1.Range() {
					b2.Set(v)
				}
				if b1.kb[0] != b2.kb[0] {
					t.Errorf("bad range: round-trip failed with pattern %b", i)
				}
			}
		})
	})

	t.Run("RangeBetweenAnd", func(t *testing.T) {
		var zero bitmap[uint32]
		for _, tc := range [][2]uint32{
			{0, 0},
			{0, 1},
			{1, 1},
			{1, 2},
			{0, 100},
			{25, 75},
		} {
			if act := slices.Collect(zero.RangeBetweenAnd(tc[0], tc[1], nilBitmap[uint32]())); len(act) != 0 {
				t.Errorf("range of empty bitmap should be empty, got %v", act)
			}
		}

		b := bitmapOf(0, 1, 63, 64, 65, 127, 128, 200)
		for _, tc := range []struct {
			start, end uint32
			flt        bitmap[uint32]
			want       []uint32
		}{
			{0, 201, nilBitmap[uint32](), []uint32{0, 1, 63, 64, 65, 127, 128, 200}}, // all
			{0, 201, bitmapOf(0, 1, 2, 64, 201, 300), []uint32{0, 1, 64}},            // filtered
			{0, 200, nilBitmap[uint32](), []uint32{0, 1, 63, 64, 65, 127, 128}},      // end exclusive
			{1, 64, nilBitmap[uint32](), []uint32{1, 63}},                            // within first block
			{64, 128, nilBitmap[uint32](), []uint32{64, 65, 127}},                    // within second block
			{63, 65, nilBitmap[uint32](), []uint32{63, 64}},                          // crosses block boundary
			{0, 0, nilBitmap[uint32](), nil},                                         // empty range
			{5, 5, nilBitmap[uint32](), nil},                                         // empty range
			{10, 5, nilBitmap[uint32](), nil},                                        // inverted range
			{50, 63, nilBitmap[uint32](), []uint32{}},                                // no set bits in range
			{201, 300, nilBitmap[uint32](), nil},                                     // beyond all set bits
		} {
			if tc.flt.IsNil() {
				got := slices.Collect(b.RangeBetweenAnd(tc.start, tc.end, b))
				if !slices.Equal(got, tc.want) && !(len(got) == 0 && len(tc.want) == 0) {
					t.Errorf("RangeBetweenAnd(%d, %d, self) = %v, want %v", tc.start, tc.end, got, tc.want)
				}
			}
			got := slices.Collect(b.RangeBetweenAnd(tc.start, tc.end, tc.flt))
			if !slices.Equal(got, tc.want) && !(len(got) == 0 && len(tc.want) == 0) {
				t.Errorf("RangeBetweenAnd(%d, %d, %v) = %v, want %v", tc.start, tc.end, slices.Collect(tc.flt.Range()), got, tc.want)
			}
		}

		t.Run("Blocks", func(t *testing.T) {
			b := bitmapOf(63, 64, 127, 128)
			for _, tc := range []struct {
				start, end uint32
				want       []uint32
			}{
				{64, 128, []uint32{64, 127}},
				{63, 129, []uint32{63, 64, 127, 128}},
				{64, 64, nil},
				{0, 64, []uint32{63}},
				{128, 129, []uint32{128}},
			} {
				got := slices.Collect(b.RangeBetweenAnd(tc.start, tc.end, nilBitmap[uint32]()))
				if !slices.Equal(got, tc.want) && !(len(got) == 0 && len(tc.want) == 0) {
					t.Errorf("RangeBetweenAnd(%d, %d, nil) = %v, want %v", tc.start, tc.end, got, tc.want)
				}
			}
		})

		t.Run("Chunks", func(t *testing.T) {
			// stuff is done in 4-bit chunks, so exhaustively verify the possibilities up to 2.5 chunks
			for i := range uint64(0b11_1111_1111) {
				b1 := makeBitmap[uint32](64)
				b1.kb[0] = i
				b2 := makeBitmap[uint32](64)
				for v := range b1.RangeBetweenAnd(0, 64, nilBitmap[uint32]()) {
					b2.Set(v)
				}
				if b1.kb[0] != b2.kb[0] {
					t.Errorf("bad range: round-trip failed with pattern %b", i)
				}
			}
		})
	})

	t.Run("Prev", func(t *testing.T) {
		var zero bitmap[uint32]
		for i := range 128 {
			if _, ok := zero.Prev(uint32(i)); ok {
				t.Error("Prev on empty bitmap should return false")
			}
		}

		b := bitmapOf(0, 1, 63, 64, 128, 200)
		for _, tc := range []struct {
			i      uint32
			wantV  uint32
			wantOK bool
		}{
			{0, 0, true},
			{1, 1, true},
			{62, 1, true},
			{63, 63, true},
			{64, 64, true},
			{65, 64, true},
			{127, 64, true},
			{128, 128, true},
			{199, 128, true},
			{201, 200, true},
			{200, 200, true},
			{201, 200, true},
			{300, 200, true},
		} {
			v, ok := b.Prev(tc.i)
			if ok != tc.wantOK || (ok && v != tc.wantV) {
				t.Errorf("Prev(%d) = (%d, %v), want (%d, %v)", tc.i, v, ok, tc.wantV, tc.wantOK)
			}
		}

		t.Run("Blocks", func(t *testing.T) {
			// exhaustively verify all bit when set on block boundaries
			b := bitmapOf(0, 64, 128, 192, 256)
			for i := uint32(1); i < 256; i++ {
				v, ok := b.Prev(i)
				if e := i / 64 * 64; !ok || v != e {
					t.Errorf("Prev(%d) = (%d, %v), want (%d, true)", i, v, ok, e)
				}
			}
		})
	})

	t.Run("Next", func(t *testing.T) {
		var zero bitmap[uint32]
		for i := range 128 {
			if _, ok := zero.Prev(uint32(i)); ok {
				t.Error("Next on empty bitmap should return false")
			}
		}

		b := bitmapOf(0, 1, 63, 64, 128, 200)
		for _, tc := range []struct {
			i  uint32
			v  uint32
			ok bool
		}{
			{0, 0, true},
			{1, 1, true},
			{2, 63, true},
			{63, 63, true},
			{64, 64, true},
			{65, 128, true},
			{128, 128, true},
			{129, 200, true},
			{200, 200, true},
			{201, 0, false},
			{300, 0, false},
		} {
			if v, ok := b.Next(tc.i); ok != tc.ok || v != tc.v {
				t.Errorf("Next(%d) = (%d, %v), want (%d, true)", tc.i, v, ok, tc.v)
			}
		}

		t.Run("Blocks", func(t *testing.T) {
			// exhaustively verify all bit when set on block boundaries
			b := bitmapOf(0, 64, 128, 192, 256)
			for i := uint32(1); i < 256; i++ {
				v, ok := b.Next(i)
				if e := (i + 63) / 64 * 64; !ok || v != e {
					t.Errorf("Next(%d) = (%d, %v), want (%d, true)", i, v, ok, e)
				}
			}
		})
	})
}
