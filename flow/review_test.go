package flow

import (
	"sync/atomic"
	"testing"
	"time"
)

// Checks: FLOW-17
func TestOnCloseRunsForFlowsClosedByTheCaller(t *testing.T) {
	ways := map[string]func(*Table[string, int], *Flow[int]){
		"Remove": func(tb *Table[string, int], f *Flow[int]) { tb.Remove("k", f.ID()) },
		"Sweep":  func(tb *Table[string, int], _ *Flow[int]) { tb.Sweep(time.Now().Add(time.Hour)) },
		"Close":  func(tb *Table[string, int], _ *Flow[int]) { tb.Close() },
	}
	for name, leave := range ways {
		var calls atomic.Int32
		tb := NewTable(TableOptions[string, int]{IdleTimeout: time.Minute, OnClose: func(string, *Flow[int]) { calls.Add(1) }})
		f, _ := tb.GetOrOpen("k", nil)
		if !f.Close() { // the caller closes it first
			t.Fatal("Close")
		}
		leave(tb, f)
		if tb.Len() != 0 {
			t.Fatalf("%s: flow still in the table", name)
		}
		if n := calls.Load(); n != 1 {
			t.Fatalf("%s: OnClose called %d times, want 1", name, n)
		}
		tb.Close() // leaving twice is impossible; nothing more is called
		tb.Sweep(time.Now().Add(2 * time.Hour))
		if n := calls.Load(); n != 1 {
			t.Fatalf("%s: OnClose called %d times after more removals", name, n)
		}
		if f.State() != Closed {
			t.Fatalf("%s: state %v", name, f.State())
		}
	}
}

// usable fails the test if the shard holding k stays locked.
func usable(t *testing.T, tb *Table[string, int], k string) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		tb.GetOrOpen(k, nil)
		tb.Get(k)
		tb.Len()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("shard still locked after the callback panicked")
	}
}

func mustPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected the panic to propagate")
		}
	}()
	fn()
}

// Checks: FLOW-18
func TestGetOrOpenPanicReleasesShard(t *testing.T) {
	tb := NewTable(TableOptions[string, int]{Shards: 1})
	mustPanic(t, func() { tb.GetOrOpen("k", func() int { panic("newValue") }) })
	usable(t, tb, "k")
	if f, ok := tb.Get("k"); !ok || f.Value != 0 {
		t.Fatal("GetOrOpen after a panic did not create the flow normally")
	}
	if tb.Len() != 1 {
		t.Fatalf("Len %d: the panicking call must not have inserted a flow", tb.Len())
	}
}

// Checks: FLOW-19
func TestRangePanicReleasesShard(t *testing.T) {
	tb := NewTable(TableOptions[string, int]{Shards: 1})
	tb.GetOrOpen("a", nil)
	mustPanic(t, func() { tb.Range(func(string, *Flow[int]) bool { panic("fn") }) })
	usable(t, tb, "a")
	n := 0
	tb.Range(func(string, *Flow[int]) bool { n++; return true })
	if n != 1 {
		t.Fatalf("Range after a panic saw %d flows", n)
	}
}
