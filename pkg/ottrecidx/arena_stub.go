//go:build !goexperiment.arenas

package ottrecidx

import (
	"strconv"
	"sync/atomic"
	"unsafe"
)

type arena struct {
	alloc atomic.Uint64
}

func newArena() *arena {
	return &arena{}
}

func (a *arena) New[T any]() *T {
	v := new(T)
	a.alloc.Add(uint64(unsafe.Sizeof(*v)))
	return v
}

func (a *arena) MakeSlice[T any](len, cap int) []T {
	v := make([]T, len, cap)
	a.alloc.Add(uint64(unsafeSizeofSlice(v)))
	return v
}

func (a *arena) String() string {
	return "arena[stub]{alloc:" + strconv.FormatUint(a.alloc.Add(0), 10) + "}"
}

func unsafeSizeofSlice[T any](v []T) uintptr {
	if cap(v) != 0 {
		return unsafe.Sizeof(v[0]) * uintptr(cap(v))
	}
	return 0
}
