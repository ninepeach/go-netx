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
