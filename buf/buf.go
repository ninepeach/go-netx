// Package buf provides pooled byte buffers with explicit ownership.
//
// Ownership rules:
//
//  1. Borrowed: data handed to a callback (for example udp.Packet.Data) is
//     valid only until the callback returns. Never Release it; Copy it to keep it.
//  2. Owned: a *Buf from Get or Copy has exactly one owner, who must call
//     Release exactly once.
//  3. Transfer: handing a *Buf to someone else (for example pushing it into a
//     queue) transfers ownership. If the transfer fails, ownership stays with
//     the caller, who must still Release it.
//
// Build with -tags netxdebug to panic on double Release and to count
// outstanding buffers.
package buf

import (
	"sync"

	"github.com/ninepeach/go-netx/limit"
)

// Size classes. Few classes keep pool hit rates high.
const (
	Small  = 2 << 10  // one MTU-sized datagram
	Medium = 16 << 10 // typical stream read
	Large  = 64 << 10 // largest UDP datagram
)

var classes = [...]int{Small, Medium, Large}

// Buf is an owned byte buffer. B has the length requested from Get.
type Buf struct {
	B     []byte
	full  []byte
	class int8 // index into classes, -1 if unpooled
	pool  *Pool
	debugState
}

// Pool hands out buffers in three size classes. A Pool is safe for
// concurrent use. The zero value is not usable; call NewPool.
type Pool struct {
	pools  [len(classes)]sync.Pool
	budget *limit.Budget
}

// NewPool returns a pool. If budget is non-nil, Get charges each buffer's
// capacity against it and Release refunds it; Get returns nil when the
// budget is exhausted.
//
// The budget therefore caps in-use buffer memory only. Released buffers may
// stay in the pool (sync.Pool) until the GC reclaims them, so retained pool
// memory and total process memory can exceed the budget. Use the budget to
// bound how much data the gateway holds at once, not as a process RSS limit.
func NewPool(budget *limit.Budget) *Pool {
	p := &Pool{budget: budget}
	for i := range classes {
		size, class := classes[i], int8(i)
		p.pools[i].New = func() any {
			return &Buf{full: make([]byte, size), class: class, pool: p}
		}
	}
	return p
}

// Get returns a buffer with len(B) == n, or nil if the budget is exhausted.
// Requests larger than Large are allocated directly and not pooled.
// Contents are not zeroed.
func (p *Pool) Get(n int) *Buf {
	if n < 0 {
		panic("buf: negative size")
	}
	class := -1
	for i, size := range classes {
		if n <= size {
			class = i
			break
		}
	}
	capacity := n
	if class >= 0 {
		capacity = classes[class]
	}
	if p.budget != nil && !p.budget.Reserve(int64(capacity)) {
		return nil
	}
	var b *Buf
	if class >= 0 {
		b = p.pools[class].Get().(*Buf)
	} else {
		b = &Buf{full: make([]byte, n), class: -1, pool: p}
	}
	b.B = b.full[:n]
	b.onGet()
	return b
}

// Copy returns an owned copy of data, or nil if the budget is exhausted.
func (p *Pool) Copy(data []byte) *Buf {
	b := p.Get(len(data))
	if b != nil {
		copy(b.B, data)
	}
	return b
}

// Release returns the buffer to its pool. The caller must not touch b or
// b.B afterwards. Releasing twice is a bug.
func (b *Buf) Release() {
	b.onRelease()
	p := b.pool
	if p.budget != nil {
		p.budget.Release(int64(cap(b.full)))
	}
	b.B = nil
	if b.class >= 0 {
		p.pools[b.class].Put(b)
	}
}
