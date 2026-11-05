package udp

import "testing"

func TestDropCounterWrapAndStaleReports(t *testing.T) {
	var d dropCounter
	d.last.Store(0xfffffff0)

	d.observe(5) // wraps through zero: 21 new drops
	if got := d.total.Load(); got != 21 {
		t.Fatalf("after wrap total=%d, want 21", got)
	}

	d.observe(0xfffffff5) // delayed report from before the wrap
	if got := d.total.Load(); got != 21 {
		t.Fatalf("stale report changed total to %d", got)
	}

	d.observe(10) // five more drops
	if got := d.total.Load(); got != 26 {
		t.Fatalf("total=%d, want 26", got)
	}

	d.observe(10) // duplicate report
	if got := d.total.Load(); got != 26 {
		t.Fatalf("duplicate report changed total to %d", got)
	}
}
