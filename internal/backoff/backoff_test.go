package backoff

import (
	"testing"
	"time"
)

// Checks: TCP-08, UDP-09
func TestNextDoublesAndCaps(t *testing.T) {
	var d time.Duration
	want := []time.Duration{Min, 2 * Min, 4 * Min}
	for i, w := range want {
		if d = Next(d); d != w {
			t.Fatalf("step %d: %v, want %v", i, d, w)
		}
	}
	for i := 0; i < 20; i++ {
		d = Next(d)
	}
	if d != Max {
		t.Fatalf("not capped: %v", d)
	}
}

// Checks: TCP-07, UDP-10
func TestWaitStopsEarly(t *testing.T) {
	stop := make(chan struct{})
	close(stop)
	start := time.Now()
	if Wait(time.Hour, stop) {
		t.Fatal("reported a full wait")
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("did not return promptly")
	}
	if !Wait(time.Millisecond, make(chan struct{})) {
		t.Fatal("short wait reported as stopped")
	}
}
