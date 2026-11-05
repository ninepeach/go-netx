package udp

import (
	"math"
	"testing"
)

// Checks: UDP-28, UDP-29
func TestDropCounterWraps(t *testing.T) {
	var d dropCounter
	// The kernel count only moves forward, by less than 2^31 between reads.
	d.observe(1 << 30)
	d.observe(1<<31 + 1<<30 - 1)
	d.observe(math.MaxUint32 - 1)
	d.observe(5) // wrapped: +7
	if got := d.total.Load(); got != math.MaxUint32-1+7 {
		t.Fatalf("total=%d", got)
	}
	d.observe(3) // stale report from another reader: ignored
	d.observe(5) // unchanged
	if got := d.total.Load(); got != math.MaxUint32-1+7 {
		t.Fatalf("total=%d after stale", got)
	}
}
