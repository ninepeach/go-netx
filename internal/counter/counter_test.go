package counter

import (
	"sync"
	"testing"
)

func TestCounterOperations(t *testing.T) {
	var c Counter
	c.Add(2)
	c.Inc()
	c.Store(10)
	c.Max(8)
	if got := c.Load(); got != 10 {
		t.Fatalf("Load=%d, want 10", got)
	}
	c.Max(12)
	if got := c.Load(); got != 12 {
		t.Fatalf("Max did not raise value: %d", got)
	}
	c.Dec()
	if got := c.Load(); got != 11 {
		t.Fatalf("Dec=%d, want 11", got)
	}
}

func TestCounterMaxConcurrent(t *testing.T) {
	var c Counter
	var wg sync.WaitGroup
	for i := uint64(1); i <= 1000; i++ {
		wg.Add(1)
		go func(n uint64) {
			defer wg.Done()
			c.Max(n)
		}(i)
	}
	wg.Wait()
	if got := c.Load(); got != 1000 {
		t.Fatalf("concurrent Max=%d, want 1000", got)
	}
}

func BenchmarkCounterInc(b *testing.B) {
	var c Counter
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Inc()
		}
	})
}
