// Package limit decides whether new work may enter (Limiter) and how much of
// a shared resource it may hold (Budget). It has no dependencies and never
// queues: every call answers immediately.
package limit

import (
	"sync"
	"sync/atomic"
	"time"
)

// Limiter is an admission check. TryAcquire never blocks. Every successful
// TryAcquire must be paired with exactly one Release.
type Limiter interface {
	TryAcquire() bool
	Release()
}

// Concurrency admits at most Max holders at a time.
type Concurrency struct {
	max   int64
	inUse atomic.Int64
}

// NewConcurrency returns a limiter that admits at most max holders.
// max must be positive.
func NewConcurrency(max int) *Concurrency {
	if max <= 0 {
		panic("limit: NewConcurrency max must be positive")
	}
	return &Concurrency{max: int64(max)}
}

func (c *Concurrency) TryAcquire() bool {
	for {
		n := c.inUse.Load()
		if n >= c.max {
			return false
		}
		if c.inUse.CompareAndSwap(n, n+1) {
			return true
		}
	}
}

// Release returns a permit. Releasing more permits than were acquired is a
// caller bug and panics, as with Budget.Release, instead of silently
// raising the limit.
func (c *Concurrency) Release() {
	if c.inUse.Add(-1) < 0 {
		c.inUse.Add(1)
		panic("limit: Concurrency.Release without matching TryAcquire")
	}
}

// InUse returns current holders. It never exceeds Max.
func (c *Concurrency) InUse() int64 { return c.inUse.Load() }
func (c *Concurrency) Max() int64   { return c.max }

// Rate is a token bucket. Release is a no-op: tokens refill with time.
type Rate struct {
	mu     sync.Mutex
	perSec float64
	burst  float64
	tokens float64
	last   time.Time
	now    func() time.Time
}

// NewRate admits perSec events per second on average with bursts up to burst.
func NewRate(perSec float64, burst int) *Rate {
	if perSec <= 0 || burst <= 0 {
		panic("limit: NewRate perSec and burst must be positive")
	}
	return &Rate{perSec: perSec, burst: float64(burst), tokens: float64(burst), now: time.Now}
}

func (r *Rate) TryAcquire() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if !r.last.IsZero() {
		r.tokens += now.Sub(r.last).Seconds() * r.perSec
		if r.tokens > r.burst {
			r.tokens = r.burst
		}
	}
	r.last = now
	if r.tokens < 1 {
		return false
	}
	r.tokens--
	return true
}

func (r *Rate) Release() {}

// All admits only if every limiter admits. On partial success the limiters
// already acquired are released, so All never leaks a permit.
func All(limiters ...Limiter) Limiter { return all(limiters) }

type all []Limiter

func (a all) TryAcquire() bool {
	for i, l := range a {
		if !l.TryAcquire() {
			for j := i - 1; j >= 0; j-- {
				a[j].Release()
			}
			return false
		}
	}
	return true
}

func (a all) Release() {
	for _, l := range a {
		l.Release()
	}
}

// Budget caps the total amount of a resource, usually bytes, held at once.
type Budget struct {
	max  int64
	used atomic.Int64
}

// NewBudget returns a budget of max units. max must be positive.
func NewBudget(max int64) *Budget {
	if max <= 0 {
		panic("limit: NewBudget max must be positive")
	}
	return &Budget{max: max}
}

// Reserve takes n units if they fit and reports whether it did. Negative n
// is refused. The check is overflow-safe and Used never exceeds Max, even
// transiently.
func (b *Budget) Reserve(n int64) bool {
	if n < 0 {
		return false
	}
	for {
		used := b.used.Load()
		if n > b.max-used { // no overflow: 0 <= used <= max
			return false
		}
		if b.used.CompareAndSwap(used, used+n) {
			return true
		}
	}
}

// Release returns n units previously taken with Reserve. Releasing a
// negative amount, or more than is in use, is a caller bug and panics.
func (b *Budget) Release(n int64) {
	if n < 0 {
		panic("limit: Budget.Release of negative amount")
	}
	if b.used.Add(-n) < 0 {
		panic("limit: Budget.Release of more than reserved")
	}
}
func (b *Budget) Used() int64 { return b.used.Load() }
func (b *Budget) Max() int64  { return b.max }

// PerKey admits at most Max holders for each key, for example per client
// address. Keys are forgotten when their count drops to zero, so memory
// follows the keys currently held, not every key ever seen.
type PerKey[K comparable] struct {
	max int
	mu  sync.Mutex
	n   map[K]int
}

// NewPerKey returns a limiter allowing max holders per key. max must be
// positive.
func NewPerKey[K comparable](max int) *PerKey[K] {
	if max <= 0 {
		panic("limit: NewPerKey max must be positive")
	}
	return &PerKey[K]{max: max, n: make(map[K]int)}
}

// TryAcquire takes a slot for k if it has one free.
func (p *PerKey[K]) TryAcquire(k K) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.n[k] >= p.max {
		return false
	}
	p.n[k]++
	return true
}

// Release returns a slot for k. Releasing a key that holds no slot is a
// caller bug and panics.
func (p *PerKey[K]) Release(k K) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n, ok := p.n[k]
	if !ok {
		panic("limit: PerKey.Release without matching TryAcquire")
	}
	if n == 1 {
		delete(p.n, k)
		return
	}
	p.n[k] = n - 1
}

// InUse returns the slots held for k.
func (p *PerKey[K]) InUse(k K) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.n[k]
}

// Keys returns how many keys currently hold at least one slot.
func (p *PerKey[K]) Keys() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.n)
}

// Max returns the per-key limit.
func (p *PerKey[K]) Max() int { return p.max }
