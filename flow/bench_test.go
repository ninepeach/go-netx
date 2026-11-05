package flow

import (
	"fmt"
	"net/netip"
	"testing"
	"time"
)

func fill(n int) (*Table[netip.AddrPort, int], []netip.AddrPort) {
	tb := NewTable(TableOptions[netip.AddrPort, int]{IdleTimeout: time.Hour})
	keys := make([]netip.AddrPort, n)
	for i := range keys {
		ip := netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)})
		keys[i] = netip.AddrPortFrom(ip, uint16(1024+i%50000))
		tb.GetOrOpen(keys[i], nil)
	}
	return tb, keys
}

// Lookup cost as the table grows.
//
// Checks: NF-14
func BenchmarkGetBySize(b *testing.B) {
	for _, n := range []int{1_000, 100_000, 1_000_000} {
		b.Run(fmt.Sprintf("flows=%d", n), func(b *testing.B) {
			tb, keys := fill(n)
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					tb.Get(keys[i%len(keys)])
					i += 7919 // stride through keys to defeat caching
				}
			})
		})
	}
}

// Sweep cost as the table grows, with nothing to evict (the steady state).
// Per-flow cost rises with size as the table outgrows the CPU caches:
// measured about 16ns/flow at 10k and about 105ns/flow at 1M (2 vCPU).
//
// Checks: FLOW-14, NF-14
func BenchmarkSweepBySize(b *testing.B) {
	for _, n := range []int{10_000, 100_000, 1_000_000} {
		b.Run(fmt.Sprintf("flows=%d", n), func(b *testing.B) {
			tb, _ := fill(n)
			now := time.Now()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if tb.Sweep(now) != 0 {
					b.Fatal("evicted live flows")
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*n), "ns/flow")
		})
	}
}
