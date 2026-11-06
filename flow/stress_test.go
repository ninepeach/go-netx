//go:build stress

package flow

import (
	"context"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Many goroutines create, touch and remove flows while Run expires idle
// ones. Every created flow is closed exactly once, and none is closed
// twice, however the paths interleave.
//
// Checks: NF-05
func TestStressTableChurn(t *testing.T) {
	var created, closedOnce, closedTwice atomic.Int64
	seen := sync.Map{}
	tb := NewTable(TableOptions[int, int]{
		IdleTimeout: 20 * time.Millisecond,
		OnClose: func(_ int, f *Flow[int]) {
			if _, dup := seen.LoadOrStore(f.ID(), true); dup {
				closedTwice.Add(1)
			}
			closedOnce.Add(1)
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() { tb.Run(ctx); close(runDone) }()

	deadline := time.Now().Add(time.Second)
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(seed uint64) {
			defer wg.Done()
			r := rand.New(rand.NewPCG(seed, seed))
			for time.Now().Before(deadline) {
				k := r.IntN(10_000)
				switch r.IntN(4) {
				case 0:
					if _, ok := tb.GetOrOpen(k, nil); ok {
						created.Add(1)
					}
				case 1:
					tb.GetAndTouch(k) // traffic, racing Run's Sweep
				case 3:
					if f, ok := tb.Get(k); ok {
						f.Touch()
						if f.State() == Closed {
							f.Touch() // must stay a no-op
						}
					}
				case 2:
					if f, ok := tb.Get(k); ok {
						tb.Remove(k, f.ID())
					}
				}
			}
		}(uint64(g))
	}
	wg.Wait()
	cancel()
	<-runDone // Run closes everything still open
	if closedTwice.Load() != 0 {
		t.Fatalf("%d flows closed twice", closedTwice.Load())
	}
	if closedOnce.Load() != created.Load() || tb.Len() != 0 {
		t.Fatalf("created=%d closed=%d len=%d", created.Load(), closedOnce.Load(), tb.Len())
	}
	t.Logf("created and closed %d flows", created.Load())
}
