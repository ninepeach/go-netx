package queue

import (
	"context"
	"testing"
)

// Random operation sequences against a model slice: FIFO order, capacity,
// overflow policy, close semantics and the OnDrop/Stats bookkeeping must
// all agree.
//
// Checks: NF-10
func FuzzRingOps(f *testing.F) {
	f.Add(byte(0), byte(3), []byte{0, 0, 0, 0, 0, 1, 1, 2, 1, 3})
	f.Add(byte(1), byte(1), []byte{0, 0, 0, 1, 0, 2, 3, 1, 1})
	f.Fuzz(func(t *testing.T, policyByte, capByte byte, ops []byte) {
		policy := Policy(policyByte % 2)
		var droppedVals []int
		r := New(Options[int]{Cap: int(capByte%8) + 1, Policy: policy,
			OnDrop: func(v int) { droppedVals = append(droppedVals, v) }})
		capacity := r.Cap()
		var model []int
		closed := false
		var want Stats
		next := 0
		for _, op := range ops {
			switch op % 4 {
			case 0: // Push
				ok := r.Push(next)
				switch {
				case closed:
					if ok {
						t.Fatal("push accepted after close")
					}
					want.Rejected++
				case len(model) < capacity:
					if !ok {
						t.Fatal("push refused with room")
					}
					model = append(model, next)
					want.Pushed++
				case policy == Reject:
					if ok {
						t.Fatal("full Reject queue accepted")
					}
					want.Rejected++
				default: // DropOldest
					if !ok {
						t.Fatal("DropOldest refused")
					}
					model = append(model[1:], next)
					want.Pushed++
					want.Dropped++
				}
				next++
			case 1: // Pop
				v, ok := r.Pop()
				if ok != (len(model) > 0) {
					t.Fatalf("Pop ok=%v with model len %d", ok, len(model))
				}
				if ok {
					if v != model[0] {
						t.Fatalf("Pop=%d want %d (FIFO)", v, model[0])
					}
					model = model[1:]
					want.Popped++
				}
			case 2: // Close
				r.Close()
				closed = true
			case 3: // PopWait on a closed, empty queue must not block
				if closed && len(model) == 0 {
					if _, err := r.PopWait(context.Background()); err != ErrClosed {
						t.Fatalf("PopWait on closed empty queue: %v", err)
					}
				}
			}
			if r.Len() != len(model) || r.Len() > capacity {
				t.Fatalf("Len=%d model=%d cap=%d", r.Len(), len(model), capacity)
			}
		}
		left := r.Drain()
		want.Dropped += uint64(left)
		if left != len(model) {
			t.Fatalf("Drain=%d model=%d", left, len(model))
		}
		if got := r.Stats(); got != want {
			t.Fatalf("stats %+v want %+v", got, want)
		}
		if uint64(len(droppedVals)) != want.Dropped {
			t.Fatalf("OnDrop called %d times, Dropped=%d", len(droppedVals), want.Dropped)
		}
	})
}
