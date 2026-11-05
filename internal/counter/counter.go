// Package counter provides cache-line padded atomic counters for hot paths.
package counter

import "sync/atomic"

// Counter is an atomic uint64 padded to its own cache line so that counters
// owned by different goroutines never share a line.
type Counter struct {
	_ [64]byte
	v atomic.Uint64
	_ [56]byte
}

func (c *Counter) Add(n uint64) { c.v.Add(n) }
func (c *Counter) Inc()         { c.v.Add(1) }
func (c *Counter) Load() uint64 { return c.v.Load() }
func (c *Counter) Store(n uint64) {
	c.v.Store(n)
}

// Max raises the counter to n if n is larger.
func (c *Counter) Max(n uint64) {
	for {
		cur := c.v.Load()
		if n <= cur || c.v.CompareAndSwap(cur, n) {
			return
		}
	}
}

// Dec subtracts one. Use it only for gauges that never go below zero.
func (c *Counter) Dec() { c.v.Add(^uint64(0)) }
