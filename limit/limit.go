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
	if c.inUse.Add(1) > c.max {
		c.inUse.Add(-1)
		return false
	}
	return true
}

func (c *Concurrency) Release() { c.inUse.Add(-1) }

// InUse returns current holders. Concurrent failed attempts may make it
// briefly exceed Max.
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

// Reserve takes n units if they fit and reports whether it did.
func (b *Budget) Reserve(n int64) bool {
	if b.used.Add(n) > b.max {
		b.used.Add(-n)
		return false
	}
	return true
}

// Release returns n units previously taken with Reserve.
func (b *Budget) Release(n int64) { b.used.Add(-n) }
func (b *Budget) Used() int64     { return b.used.Load() }
func (b *Budget) Max() int64      { return b.max }
