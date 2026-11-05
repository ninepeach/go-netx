package limit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrency(t *testing.T) {
	c := NewConcurrency(2)
	if !c.TryAcquire() || !c.TryAcquire() {
		t.Fatal("expected two permits")
	}
	if c.TryAcquire() {
		t.Fatal("third permit admitted")
	}
	c.Release()
	if !c.TryAcquire() {
		t.Fatal("permit not returned")
	}
}

func TestConcurrencyParallel(t *testing.T) {
	c := NewConcurrency(8)
	var wg sync.WaitGroup
	var holders, peak atomic.Int64
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				if c.TryAcquire() {
					v := holders.Add(1)
					for p := peak.Load(); v > p && !peak.CompareAndSwap(p, v); p = peak.Load() {
					}
					holders.Add(-1)
					c.Release()
				}
			}
		}()
	}
	wg.Wait()
	if peak.Load() > 8 || c.InUse() != 0 {
		t.Fatalf("peak=%d inUse=%d", peak.Load(), c.InUse())
	}
}

func TestRate(t *testing.T) {
	now := time.Unix(0, 0)
	r := NewRate(10, 2)
	r.now = func() time.Time { return now }
	if !r.TryAcquire() || !r.TryAcquire() || r.TryAcquire() {
		t.Fatal("burst not enforced")
	}
	now = now.Add(100 * time.Millisecond)
	if !r.TryAcquire() || r.TryAcquire() {
		t.Fatal("refill not 1 token per 100ms")
	}
}

func TestAllReleasesOnPartialFailure(t *testing.T) {
	a, b := NewConcurrency(1), NewConcurrency(1)
	b.TryAcquire()
	if All(a, b).TryAcquire() {
		t.Fatal("admitted while b full")
	}
	if a.InUse() != 0 {
		t.Fatal("a leaked a permit")
	}
}

func TestBudget(t *testing.T) {
	b := NewBudget(100)
	if !b.Reserve(60) || b.Reserve(50) || !b.Reserve(40) {
		t.Fatal("budget arithmetic")
	}
	b.Release(100)
	if b.Used() != 0 {
		t.Fatal("not released")
	}
}
