// Package buf provides pooled byte buffers with cursors and headroom.
//
//	[ headroom | data | free ]
//	0        start   end    Cap
//
// A *Buf has one owner, who calls Release exactly once. Slices returned by
// its methods alias the buffer: they are valid until the next call that
// changes it, and never after Release. Memory is not zeroed: write before
// you read or send.
package buf

import (
	"math"
	"math/bits"
	"sync"
)

// Large is the largest power-of-two pooled size. Requests through 72KiB use
// one additional special pool class; bigger buffers are allocated directly.
const Large = 64 << 10

const (
	minShift     = 6  // the smallest class is 64 bytes
	powerClasses = 11 // 64B, 128B, … 64KiB
	maxPooled    = 72 << 10
	classes      = powerClasses + 1
)

var pools [classes]sync.Pool

func init() {
	for i := range pools {
		size, class := classSize(i), int8(i)
		pools[i].New = func() any {
			b := &Buf{data: make([]byte, size), class: class}
			b.owner = b
			return b
		}
	}
}

// classOf returns the class holding n bytes, or -1 above 72KiB.
func classOf(n int) int {
	switch {
	case n > maxPooled:
		return -1
	case n > Large:
		return powerClasses
	case n <= 1<<minShift:
		return 0
	}
	return bits.Len(uint(n-1)) - minShift
}

func classSize(class int) int {
	if class == powerClasses {
		return maxPooled
	}
	return 1 << (minShift + class)
}

// Capacity returns the memory a buffer of n bytes occupies: its class
// size, or n above 72KiB.
func Capacity(n int) int {
	if n < 0 {
		panic("buf: negative size")
	}
	if c := classOf(n); c >= 0 {
		return classSize(c)
	}
	return n
}

// Buf is a byte buffer. Its zero value is empty and has no writable space.
// A Buf must not be copied or used concurrently.
type Buf struct {
	data       []byte // the whole block
	start, end int
	size       int  // head + requested size; 0 once released
	head       int  // start after Reset
	class      int8 // -1 if not pooled
	released   bool
	owner      *Buf // identifies the original handle at Release
}

// Get returns an empty buffer with room for size bytes.
func Get(size int) *Buf { return GetHeadroom(0, size) }

// GetHeadroom returns an empty buffer with head bytes in front of the data
// for ExtendHeader and room for size bytes after it. Cap is head+size.
func GetHeadroom(head, size int) *Buf {
	if head < 0 || size < 0 || head > math.MaxInt-size {
		panic("buf: invalid size or headroom")
	}
	n := head + size
	var b *Buf
	if c := classOf(n); c >= 0 {
		b = pools[c].Get().(*Buf)
	} else {
		b = WrapWritable(make([]byte, n))
	}
	b.start, b.end, b.head, b.size, b.released = head, head, head, n, false
	return b
}

// Copy returns a buffer holding a copy of p.
func Copy(p []byte) *Buf {
	b := Get(len(p))
	copy(b.Extend(len(p)), p)
	return b
}

// Wrap aliases data as already-readable content without copying it.
// The caller must not access data independently until Release.
func Wrap(data []byte) *Buf {
	b := WrapWritable(data)
	b.end = len(data)
	return b
}

// WrapWritable aliases data as empty writable storage with capacity len(data).
// The caller must keep data available and avoid accessing it independently
// while the buffer owns access. Release drops the alias without clearing data.
func WrapWritable(data []byte) *Buf {
	b := &Buf{data: data[:len(data):len(data)], size: len(data), class: -1}
	b.owner = b
	return b
}

// Release ends ownership of b. A second Release panics.
func (b *Buf) Release() {
	if b.released {
		panic("buf: double Release")
	}
	if b.owner != nil && b.owner != b {
		panic("buf: release of copied Buf")
	}
	b.released = true
	b.start, b.end, b.head, b.size = 0, 0, 0, 0
	if b.owner == nil || b.class < 0 {
		b.data = nil
		return
	}
	pools[b.class].Put(b)
}

// Len returns the number of data bytes.
func (b *Buf) Len() int { return b.end - b.start }

// Cap returns how far the data may extend, headroom included.
func (b *Buf) Cap() int { return b.size }

// Bytes returns the data.
func (b *Buf) Bytes() []byte { return b.data[b.start:b.end:b.end] }

// FreeBytes returns the space after the data. Read into it, then Extend.
func (b *Buf) FreeBytes() []byte { return b.data[b.end:b.size:b.size] }

// Extend appends n bytes to the data and returns them.
func (b *Buf) Extend(n int) []byte {
	if n < 0 || n > b.size-b.end || b.released {
		panic("buf: Extend out of range")
	}
	b.end += n
	return b.data[b.end-n : b.end : b.end]
}

// ExtendHeader prepends n bytes from the headroom and returns them.
func (b *Buf) ExtendHeader(n int) []byte {
	if n < 0 || n > b.start || b.released {
		panic("buf: ExtendHeader out of range")
	}
	b.start -= n
	return b.data[b.start : b.start+n : b.start+n]
}

// Advance consumes n bytes from the front; they become headroom.
func (b *Buf) Advance(n int) {
	if n < 0 || n > b.end-b.start || b.released {
		panic("buf: Advance out of range")
	}
	b.start += n
}

// Truncate keeps the first n bytes of the data.
func (b *Buf) Truncate(n int) {
	if n < 0 || n > b.end-b.start || b.released {
		panic("buf: Truncate out of range")
	}
	b.end = b.start + n
}

// Reset empties the buffer and restores its original headroom.
func (b *Buf) Reset() {
	b.checkLive()
	b.start, b.end = b.head, b.head
}

func (b *Buf) checkLive() {
	if b.released {
		panic("buf: use after Release")
	}
}
