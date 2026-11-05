// Package queue provides a bounded ring buffer with explicit overflow policy.
// It is the shared backpressure primitive: a full queue either rejects the
// new item (the producer keeps ownership and must slow down) or evicts the
// oldest item through OnDrop. Nothing grows without bound.
package queue

import (
	"context"
	"errors"
	"sync"
)

// ErrClosed is returned by waits on a closed queue.
var ErrClosed = errors.New("queue: closed")

// Policy decides what Push does when the queue is full.
type Policy uint8

const (
	// Reject refuses the new item. Push returns false and the caller keeps
	// ownership of it.
	Reject Policy = iota
	// DropOldest evicts the oldest item, hands it to OnDrop, and accepts
	// the new one.
	DropOldest
)

// Options configures a Ring.
type Options[T any] struct {
	// Cap is rounded up to a power of two. It must be positive.
	Cap    int
	Policy Policy
	// OnDrop receives every item the queue discards: evicted items under
	// DropOldest and items left in the queue when Drain is called. Use it to
	// release owned resources such as *buf.Buf.
	OnDrop func(T)
}

// Stats is a point-in-time snapshot.
type Stats struct {
	Len      int
	Pushed   uint64
	Popped   uint64
	Dropped  uint64 // evicted under DropOldest or drained
	Rejected uint64 // refused under Reject or after Close
}

// Ring is a bounded multi-producer multi-consumer FIFO. It is safe for
// concurrent use.
type Ring[T any] struct {
	mu       sync.Mutex
	items    []T
	mask     int
	head     int // next pop
	n        int
	closed   bool
	policy   Policy
	onDrop   func(T)
	notEmpty chan struct{} // cap 1, signalled after push
	notFull  chan struct{} // cap 1, signalled after pop
	done     chan struct{} // closed by Close
	stats    Stats
}

// New returns a ring.
func New[T any](o Options[T]) *Ring[T] {
	if o.Cap <= 0 {
		panic("queue: Cap must be positive")
	}
	size := 1
	for size < o.Cap {
		size <<= 1
	}
	return &Ring[T]{
		items: make([]T, size), mask: size - 1, policy: o.Policy, onDrop: o.OnDrop,
		notEmpty: make(chan struct{}, 1), notFull: make(chan struct{}, 1),
		done: make(chan struct{}),
	}
}

func signal(c chan struct{}) {
	select {
	case c <- struct{}{}:
	default:
	}
}

// Push adds v without blocking and reports whether v was accepted. On false
// the caller still owns v.
func (r *Ring[T]) Push(v T) bool {
	var dropped T
	evicted := false
	r.mu.Lock()
	if r.closed {
		r.stats.Rejected++
		r.mu.Unlock()
		return false
	}
	if r.n == len(r.items) {
		if r.policy == Reject {
			r.stats.Rejected++
			r.mu.Unlock()
			return false
		}
		dropped, evicted = r.popLocked(), true
		r.stats.Dropped++
	}
	r.items[(r.head+r.n)&r.mask] = v
	r.n++
	r.stats.Pushed++
	r.mu.Unlock()
	signal(r.notEmpty)
	if evicted && r.onDrop != nil {
		r.onDrop(dropped)
	}
	return true
}

// PushWait blocks until there is room, ignoring Policy. It returns ctx.Err()
// or ErrClosed on failure, in which case the caller still owns v. Never call
// it from a network read loop: a slow consumer would stall all traffic.
func (r *Ring[T]) PushWait(ctx context.Context, v T) error {
	for {
		r.mu.Lock()
		if r.closed {
			r.stats.Rejected++
			r.mu.Unlock()
			return ErrClosed
		}
		if r.n < len(r.items) {
			r.items[(r.head+r.n)&r.mask] = v
			r.n++
			r.stats.Pushed++
			more := r.n < len(r.items)
			r.mu.Unlock()
			signal(r.notEmpty)
			if more {
				signal(r.notFull) // let another waiting producer proceed
			}
			return nil
		}
		r.mu.Unlock()
		select {
		case <-r.notFull:
		case <-r.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (r *Ring[T]) popLocked() T {
	var zero T
	v := r.items[r.head]
	r.items[r.head] = zero
	r.head = (r.head + 1) & r.mask
	r.n--
	return v
}

// Pop removes the oldest item without blocking.
func (r *Ring[T]) Pop() (T, bool) {
	r.mu.Lock()
	if r.n == 0 {
		r.mu.Unlock()
		var zero T
		return zero, false
	}
	v := r.popLocked()
	r.stats.Popped++
	more := r.n > 0
	r.mu.Unlock()
	signal(r.notFull)
	if more {
		signal(r.notEmpty) // let another waiting consumer proceed
	}
	return v, true
}

// PopWait blocks until an item is available. After Close it keeps returning
// remaining items, then ErrClosed.
func (r *Ring[T]) PopWait(ctx context.Context) (T, error) {
	for {
		if v, ok := r.Pop(); ok {
			return v, nil
		}
		r.mu.Lock()
		closed := r.closed && r.n == 0
		r.mu.Unlock()
		if closed {
			var zero T
			return zero, ErrClosed
		}
		select {
		case <-r.notEmpty:
		case <-r.done:
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		}
	}
}

// Close stops accepting items and wakes all waiters. Items already queued
// can still be popped.
func (r *Ring[T]) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	r.mu.Unlock()
	close(r.done)
}

// Drain removes every remaining item and hands each to OnDrop. Call it after
// Close when the consumer stops, so owned items are released.
func (r *Ring[T]) Drain() int {
	r.mu.Lock()
	items := make([]T, 0, r.n)
	for r.n > 0 {
		items = append(items, r.popLocked())
	}
	r.stats.Dropped += uint64(len(items))
	r.mu.Unlock()
	if r.onDrop != nil {
		for _, v := range items {
			r.onDrop(v)
		}
	}
	return len(items)
}

func (r *Ring[T]) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

func (r *Ring[T]) Cap() int { return len(r.items) }

func (r *Ring[T]) Stats() Stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.stats
	s.Len = r.n
	return s
}
