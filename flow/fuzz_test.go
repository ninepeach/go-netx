package flow

import "testing"

// Random operation sequences against a model map: the table must agree with
// the model on which key maps to which flow, Remove must honour epochs, and
// OnClose must fire exactly once per flow that leaves.
//
// Checks: NF-09
func FuzzTableOps(f *testing.F) {
	f.Add([]byte{0, 1, 0, 1, 2, 1, 1, 1})
	f.Add([]byte{0, 0, 3, 0, 0, 0, 2, 0, 4, 0})
	f.Fuzz(func(t *testing.T, ops []byte) {
		closed := map[uint64]int{}
		tb := NewTable(TableOptions[byte, int]{
			Shards:  4,
			OnClose: func(_ byte, fl *Flow[int]) { closed[fl.ID()]++ },
		})
		model := map[byte]uint64{} // key → current flow ID
		var removed []uint64       // IDs that left the table
		for i := 0; i+1 < len(ops); i += 2 {
			op, key := ops[i]%5, ops[i+1]%8
			switch op {
			case 0: // GetOrOpen
				fl, created := tb.GetOrOpen(key, nil)
				id, had := model[key]
				if created == had || (had && fl.ID() != id) {
					t.Fatalf("GetOrOpen(%d): created=%v id=%d model=%d,%v", key, created, fl.ID(), id, had)
				}
				model[key] = fl.ID()
			case 1: // Get
				fl, ok := tb.Get(key)
				id, had := model[key]
				if ok != had || (ok && fl.ID() != id) {
					t.Fatalf("Get(%d) disagrees with model", key)
				}
			case 2: // Remove with the current ID
				if id, had := model[key]; had {
					if !tb.Remove(key, id) {
						t.Fatalf("Remove(%d, current) failed", key)
					}
					delete(model, key)
					removed = append(removed, id)
				}
			case 3: // Remove with a stale ID must never remove
				if len(removed) > 0 {
					stale := removed[int(key)%len(removed)]
					if tb.Remove(key, stale) {
						t.Fatalf("stale ID %d removed key %d", stale, key)
					}
				}
			case 4: // Close everything
				for k, id := range model {
					removed = append(removed, id)
					delete(model, k)
				}
				tb.Close()
			}
			if tb.Len() != len(model) {
				t.Fatalf("Len=%d model=%d", tb.Len(), len(model))
			}
		}
		for _, id := range removed {
			if closed[id] != 1 {
				t.Fatalf("flow %d OnClose called %d times", id, closed[id])
			}
		}
		if len(closed) != len(removed) {
			t.Fatalf("OnClose for %d flows, %d removed", len(closed), len(removed))
		}
	})
}
