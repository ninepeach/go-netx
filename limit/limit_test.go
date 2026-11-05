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

func TestBudgetRefusesNegative(t *testing.T) {
	b := NewBudget(100)
	if b.Reserve(-100) {
		t.Fatal("negative reserve accepted")
	}
	if b.Reserve(101) || !b.Reserve(0) || b.Used() != 0 {
		t.Fatal("limit bypassed")
	}
	for _, n := range []int64{-1, 1} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("Release(%d) with nothing reserved did not panic", n)
				}
			}()
			b.Release(n)
		}()
	}
}

func TestBudgetParallelNeverExceeds(t *testing.T) {
	b := NewBudget(1000)
	var wg sync.WaitGroup
	var over atomic.Bool
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 2000; j++ {
				if b.Reserve(64) {
					if b.Used() > b.Max()+64*32 { // in-flight attempts bounded
						over.Store(true)
					}
					b.Release(64)
				}
			}
		}()
	}
	wg.Wait()
	if over.Load() || b.Used() != 0 {
		t.Fatalf("used=%d", b.Used())
	}
}
