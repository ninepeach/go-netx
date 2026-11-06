package flow

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// clockTable returns a table with a fixed idle timeout whose clock the test
// moves by hand (advance), so no test depends on sleeping.
func clockTable(t *testing.T, onClose func(int, *Flow[int])) (*Table[int, int], func(time.Duration) int64) {
	t.Helper()
	tb := NewTable(TableOptions[int, int]{IdleTimeout: time.Minute, OnClose: onClose})
	base := time.Now().Add(time.Hour).UnixNano() // ahead of NewTable's clock
	tb.advance(base)
	return tb, func(d time.Duration) int64 {
		ns := base + int64(d)
		tb.advance(ns)
		return ns &^ frozen
	}
}

// Checks: FLOW-20
func TestGetDoesNotTouch(t *testing.T) {
	tb, at := clockTable(t, nil)
	f, _ := tb.GetOrOpen(1, nil)
	created := f.LastSeen()
	at(30 * time.Second)
	for i := 0; i < 3; i++ {
		if g, ok := tb.Get(1); !ok || g != f {
			t.Fatal("Get did not find the flow")
		}
	}
	if f.LastSeen() != created {
		t.Fatalf("Get changed LastSeen: %d -> %d", created, f.LastSeen())
	}
}

// Checks: FLOW-21
func TestGetOrOpenExistingDoesNotTouch(t *testing.T) {
	tb, at := clockTable(t, nil)
	f, _ := tb.GetOrOpen(1, nil)
	created := f.LastSeen()
	at(30 * time.Second)
	g, isNew := tb.GetOrOpen(1, func() int { t.Fatal("newValue called for an existing key"); return 0 })
	if isNew || g != f || f.LastSeen() != created {
		t.Fatalf("existing lookup: new=%v same=%v LastSeen %d -> %d", isNew, g == f, created, f.LastSeen())
	}
}

// Checks: FLOW-22
func TestNewFlowLastSeenInitialised(t *testing.T) {
	tb, at := clockTable(t, nil)
	now := at(10 * time.Second)
	f, isNew := tb.GetOrOpen(1, nil)
	if !isNew || f.LastSeen() != now {
		t.Fatalf("new flow LastSeen %d, want the table clock %d", f.LastSeen(), now)
	}
	before := time.Now().UnixNano()
	g := New(0) // outside a table: wall clock
	if g.LastSeen() < before&^frozen || g.LastSeen() > time.Now().UnixNano() {
		t.Fatalf("New LastSeen %d not initialised to now", g.LastSeen())
	}
}

// Checks: FLOW-23
func TestTouchUpdatesLastSeen(t *testing.T) {
	tb, at := clockTable(t, nil)
	f, _ := tb.GetOrOpen(1, nil)
	now := at(20 * time.Second)
	f.Touch()
	if f.LastSeen() != now {
		t.Fatalf("LastSeen %d after Touch, want %d", f.LastSeen(), now)
	}
	if f.State() != Open {
		t.Fatalf("Touch changed State to %v", f.State())
	}
	g, _ := tb.GetAndTouch(1)
	if g != f || f.LastSeen() != now {
		t.Fatal("GetAndTouch")
	}
	// A standalone flow touches with the wall clock and never goes back.
	h := New(0)
	prev := h.LastSeen()
	h.Touch()
	if h.LastSeen() < prev {
		t.Fatal("Touch moved LastSeen backwards")
	}
}

// Checks: FLOW-24
func TestConcurrentTouch(t *testing.T) {
	tb, at := clockTable(t, nil)
	f, _ := tb.GetOrOpen(1, nil)
	var wg sync.WaitGroup
	var last int64
	for step := 1; step <= 50; step++ {
		last = at(time.Duration(step) * time.Second)
		for g := 0; g < 8; g++ {
			wg.Add(1)
			go func() { defer wg.Done(); f.Touch(); tb.GetAndTouch(1) }()
		}
		wg.Wait()
		if f.LastSeen() != last {
			t.Fatalf("step %d: LastSeen %d, want %d", step, f.LastSeen(), last)
		}
	}
	if f.State() != Open {
		t.Fatalf("State %v", f.State())
	}
}

// Closing and Closed flows cannot be revived by Touch.
// Checks: FLOW-25
func TestTouchDoesNotReviveClosingOrClosed(t *testing.T) {
	tb, at := clockTable(t, nil)
	closing, _ := tb.GetOrOpen(1, nil)
	closed, _ := tb.GetOrOpen(2, nil)
	if !closing.BeginClose() || !closed.Close() {
		t.Fatal("close")
	}
	seenClosing, seenClosed := closing.LastSeen(), closed.LastSeen()
	at(50 * time.Second)
	closing.Touch()
	closed.Touch()
	tb.GetAndTouch(1)
	tb.GetAndTouch(2)
	if closing.LastSeen() != seenClosing || closed.LastSeen() != seenClosed {
		t.Fatal("Touch changed LastSeen of a closing or closed flow")
	}
	if closing.State() != Closing || closed.State() != Closed {
		t.Fatalf("states %v, %v", closing.State(), closed.State())
	}
	// Still idle by their old LastSeen, so Sweep removes them.
	at(2 * time.Minute)
	if n := tb.Sweep(time.Unix(0, tb.now.Load())); n != 2 {
		t.Fatalf("Sweep evicted %d, want both", n)
	}
}

// Checks: FLOW-26
func TestGetWithoutTouchStillExpires(t *testing.T) {
	var closed atomic.Int32
	tb, at := clockTable(t, func(int, *Flow[int]) { closed.Add(1) })
	tb.GetOrOpen(1, nil)
	for s := 10; s <= 70; s += 10 {
		at(time.Duration(s) * time.Second)
		tb.Get(1)
		tb.GetOrOpen(1, nil)
		tb.Range(func(int, *Flow[int]) bool { return true })
	}
	if n := tb.Sweep(time.Unix(0, tb.now.Load())); n != 1 || closed.Load() != 1 {
		t.Fatalf("lookups kept an idle flow alive: evicted %d, OnClose %d", n, closed.Load())
	}
}

// Checks: FLOW-27
func TestTouchKeepsFlowUntilIdle(t *testing.T) {
	tb, at := clockTable(t, nil)
	f, _ := tb.GetOrOpen(1, nil)
	at(50 * time.Second)
	tb.Get(1)
	f.Touch() // traffic at +50s
	if n := tb.Sweep(time.Unix(0, at(100*time.Second))); n != 0 {
		t.Fatal("evicted 50s after the last Touch, IdleTimeout is 60s")
	}
	if n := tb.Sweep(time.Unix(0, at(110*time.Second+time.Millisecond))); n != 1 {
		t.Fatal("not evicted after 60s without Touch")
	}
}

// Touch racing Sweep: either Touch lands first and the flow stays with the
// new LastSeen, or Sweep decides first and the flow leaves with its old
// LastSeen, Closed, reported once. Never anything in between.
// Checks: FLOW-28
func TestTouchRacingSweep(t *testing.T) {
	var closed atomic.Int32
	tb, at := clockTable(t, func(int, *Flow[int]) { closed.Add(1) })
	kept, evicted := 0, 0
	for i := 0; i < 2000; i++ {
		base := time.Duration(i) * 10 * time.Minute
		at(base)
		f, _ := tb.GetOrOpen(i, nil)
		old := f.LastSeen()
		now := at(base + 2*time.Minute) // idle by one minute
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); f.Touch() }()
		go func() { defer wg.Done(); tb.Sweep(time.Unix(0, now).Add(time.Second)) }()
		wg.Wait()
		_, in := tb.Get(i)
		switch {
		case in && f.State() == Open && f.LastSeen() >= now:
			kept++
		case !in && f.State() == Closed && f.LastSeen() == old:
			evicted++
			f.Touch()
			if f.LastSeen() != old {
				t.Fatal("Touch revived an evicted flow")
			}
		default:
			t.Fatalf("round %d: in=%v state=%v LastSeen=%d (old %d, now %d)", i, in, f.State(), f.LastSeen(), old, now)
		}
		tb.Remove(i, f.ID())
	}
	if int(closed.Load()) != kept+evicted {
		t.Fatalf("OnClose %d, flows %d", closed.Load(), kept+evicted)
	}
	t.Logf("Touch first %d, Sweep first %d", kept, evicted)
}

// Checks: FLOW-29
func TestTouchAfterRemoveHasNoEffect(t *testing.T) {
	tb, at := clockTable(t, nil)
	f, _ := tb.GetOrOpen(1, nil)
	seen := f.LastSeen()
	if !tb.Remove(1, f.ID()) {
		t.Fatal("Remove")
	}
	at(30 * time.Second)
	f.Touch()
	if f.LastSeen() != seen || f.State() != Closed || tb.Len() != 0 {
		t.Fatalf("after Remove: LastSeen %d (was %d), state %v, Len %d", f.LastSeen(), seen, f.State(), tb.Len())
	}
	if _, ok := tb.GetAndTouch(1); ok {
		t.Fatal("removed flow is back in the table")
	}
}
