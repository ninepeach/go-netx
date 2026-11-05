package flow

import (
	"context"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestIDsIncrease(t *testing.T) {
	a, b := New(0), New(0)
	if a.ID() == 0 || b.ID() <= a.ID() {
		t.Fatalf("ids %d %d", a.ID(), b.ID())
	}
}

func TestCloseOnce(t *testing.T) {
	f := New(0)
	if !f.BeginClose() || f.BeginClose() || f.State() != Closing {
		t.Fatal("begin close")
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if f.Close() {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 || f.State() != Closed {
		t.Fatalf("wins=%d", wins.Load())
	}
}

func TestRemoveChecksEpoch(t *testing.T) {
	var closed atomic.Int32
	tb := NewTable(TableOptions[netip.AddrPort, int]{OnClose: func(netip.AddrPort, *Flow[int]) { closed.Add(1) }})
	key := netip.MustParseAddrPort("10.0.0.1:5000")
	old, created := tb.GetOrOpen(key, nil)
	if !created {
		t.Fatal("not created")
	}
	tb.Remove(key, old.ID())
	cur, _ := tb.GetOrOpen(key, nil)
	if tb.Remove(key, old.ID()) {
		t.Fatal("stale id removed newer flow")
	}
	if !tb.Remove(key, cur.ID()) || closed.Load() != 2 {
		t.Fatal("remove current")
	}
}

func TestSweepIdle(t *testing.T) {
	tb := NewTable(TableOptions[int, int]{IdleTimeout: time.Second})
	tb.GetOrOpen(1, nil)
	if tb.Sweep(time.Now()) != 0 {
		t.Fatal("evicted fresh flow")
	}
	if tb.Sweep(time.Now().Add(2*time.Second)) != 1 || tb.Len() != 0 {
		t.Fatal("did not evict idle flow")
	}
}

func TestRunClosesAllOnExit(t *testing.T) {
	var closed atomic.Int32
	tb := NewTable(TableOptions[int, int]{OnClose: func(int, *Flow[int]) { closed.Add(1) }})
	for i := 0; i < 10; i++ {
		tb.GetOrOpen(i, nil)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tb.Run(ctx)
	if closed.Load() != 10 || tb.Len() != 0 {
		t.Fatalf("closed=%d", closed.Load())
	}
}

func BenchmarkGetAddrPort(b *testing.B) {
	tb := NewTable(TableOptions[netip.AddrPort, int]{})
	key := netip.MustParseAddrPort("10.0.0.1:5000")
	tb.GetOrOpen(key, nil)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			tb.Get(key)
		}
	})
}

// An active flow must never be evicted while it keeps being touched, with
// Run sweeping concurrently.
func TestActiveFlowSurvivesSweep(t *testing.T) {
	var evicted atomic.Int32
	tb := NewTable(TableOptions[int, int]{
		IdleTimeout: 50 * time.Millisecond,
		OnClose:     func(int, *Flow[int]) { evicted.Add(1) },
	})
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() { tb.Run(ctx); close(runDone) }()
	tb.GetOrOpen(1, nil)
	tb.GetOrOpen(2, nil) // never touched again: must be evicted
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, ok := tb.Get(1); !ok {
			t.Fatal("active flow evicted")
		}
		time.Sleep(time.Millisecond)
	}
	if _, ok := tb.Get(2); ok {
		t.Fatal("idle flow not evicted")
	}
	if evicted.Load() != 1 {
		t.Fatalf("evicted=%d", evicted.Load())
	}
	cancel()
	<-runDone
}

// Without Run, a hand-driven Sweep must still see touches as fresh.
func TestManualSweepKeepsTouchedFlow(t *testing.T) {
	tb := NewTable(TableOptions[int, int]{IdleTimeout: time.Second})
	tb.GetOrOpen(1, nil)
	now := time.Now()
	for i := 0; i < 5; i++ {
		now = now.Add(600 * time.Millisecond) // 3s total, beyond IdleTimeout
		tb.Get(1)                             // touched every interval
		if tb.Sweep(now) != 0 {
			t.Fatalf("touched flow evicted at step %d", i)
		}
	}
	if tb.Sweep(now.Add(2*time.Second)) != 1 {
		t.Fatal("idle flow not evicted")
	}
}


// GetOrOpen must construct a key's flow exactly once under concurrent access.
func TestGetOrOpenConcurrentSingleCreation(t *testing.T) {
	var created atomic.Int32
	var closed atomic.Int32
	tb := NewTable(TableOptions[int, int]{
		OnClose: func(int, *Flow[int]) { closed.Add(1) },
	})
	const workers = 64
	flows := make(chan *Flow[int], workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f, _ := tb.GetOrOpen(7, func() int {
				created.Add(1)
				return 42
			})
			flows <- f
		}()
	}
	wg.Wait()
	close(flows)
	var first *Flow[int]
	for f := range flows {
		if first == nil {
			first = f
		} else if f != first {
			t.Fatal("GetOrOpen returned different flows for the same key")
		}
	}
	if created.Load() != 1 || tb.Len() != 1 {
		t.Fatalf("created=%d len=%d", created.Load(), tb.Len())
	}
	tb.Close()
	if closed.Load() != 1 || tb.Len() != 0 {
		t.Fatalf("closed=%d len=%d", closed.Load(), tb.Len())
	}
}

// A clock advanced by a future Sweep must not move backwards on a stale Sweep.
func TestSweepClockNeverMovesBackward(t *testing.T) {
	tb := NewTable(TableOptions[int, int]{IdleTimeout: time.Second})
	f, _ := tb.GetOrOpen(1, nil)
	future := time.Now().Add(10 * time.Second)
	if got := tb.Sweep(future); got != 0 {
		t.Fatalf("first sweep evicted fresh flow: %d", got)
	}
	tb.Get(1) // touch at the advanced table clock
	if got := tb.Sweep(future.Add(-5 * time.Second)); got != 0 {
		t.Fatalf("stale sweep evicted flow: %d", got)
	}
	if got := tb.Sweep(future.Add(2 * time.Second)); got != 1 {
		t.Fatalf("future idle sweep evicted %d flows, want 1", got)
	}
}

// Remove, Sweep and Close racing for the same flow must invoke OnClose once.
func TestConcurrentRemovalClosesFlowOnce(t *testing.T) {
	var closed atomic.Int32
	tb := NewTable(TableOptions[int, int]{
		IdleTimeout: time.Nanosecond,
		OnClose:     func(int, *Flow[int]) { closed.Add(1) },
	})
	f, _ := tb.GetOrOpen(1, nil)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		tb.Remove(1, f.ID())
	}()
	go func() {
		defer wg.Done()
		tb.Sweep(time.Now().Add(time.Second))
	}()
	go func() {
		defer wg.Done()
		tb.Close()
	}()
	wg.Wait()
	if closed.Load() != 1 || tb.Len() != 0 {
		t.Fatalf("OnClose calls=%d len=%d", closed.Load(), tb.Len())
	}
}
