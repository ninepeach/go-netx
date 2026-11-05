package limit

import (
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Checks: RES-01
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

// Checks: RES-02
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

// Checks: RES-07
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

// Checks: RES-08
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

// Checks: RES-10
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

// Checks: RES-11, RES-12
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

// Checks: RES-14
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
					if b.Used() > b.Max() {
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

// Checks: RES-13
func TestBudgetOverflow(t *testing.T) {
	b := NewBudget(math.MaxInt64)
	if !b.Reserve(1) {
		t.Fatal("reserve 1")
	}
	if b.Reserve(math.MaxInt64) {
		t.Fatal("overflow bypassed the limit")
	}
	if b.Used() != 1 {
		t.Fatalf("used=%d", b.Used())
	}
	if !b.Reserve(math.MaxInt64 - 1) {
		t.Fatal("exact fit refused")
	}
}

// Checks: RES-03
func TestConcurrencyNeverExceedsMax(t *testing.T) {
	c := NewConcurrency(4)
	var wg sync.WaitGroup
	var over atomic.Bool
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 2000; j++ {
				if c.TryAcquire() {
					if c.InUse() > c.Max() {
						over.Store(true)
					}
					c.Release()
				} else if c.InUse() > c.Max() {
					over.Store(true)
				}
			}
		}()
	}
	wg.Wait()
	if over.Load() {
		t.Fatal("InUse exceeded Max")
	}
}

// Checks: RES-04
func TestConcurrencyOverReleasePanics(t *testing.T) {
	c := NewConcurrency(1)
	defer func() {
		if recover() == nil {
			t.Fatal("Release without TryAcquire did not panic")
		}
		if c.InUse() != 0 {
			t.Fatalf("InUse=%d after rejected release", c.InUse())
		}
		if !c.TryAcquire() || c.TryAcquire() {
			t.Fatal("limit raised by over-release")
		}
	}()
	c.Release()
}

func mustPanic(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatalf("%s did not panic", name)
		}
	}()
	f()
}

// Checks: RES-05
func TestNewConcurrencyRejectsNonPositive(t *testing.T) {
	mustPanic(t, "NewConcurrency(0)", func() { NewConcurrency(0) })
	mustPanic(t, "NewConcurrency(-1)", func() { NewConcurrency(-1) })
}

// Checks: RES-09
func TestNewRateRejectsNonPositive(t *testing.T) {
	mustPanic(t, "NewRate(0, 1)", func() { NewRate(0, 1) })
	mustPanic(t, "NewRate(1, 0)", func() { NewRate(1, 0) })
	mustPanic(t, "NewRate(-1, 1)", func() { NewRate(-1, 1) })
}

// Checks: RES-15
func TestNewBudgetRejectsNonPositive(t *testing.T) {
	mustPanic(t, "NewBudget(0)", func() { NewBudget(0) })
	mustPanic(t, "NewBudget(-1)", func() { NewBudget(-1) })
}
