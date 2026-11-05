package flow

import (
	"context"
	"hash/maphash"
	"sync"
	"sync/atomic"
	"time"
)

// TableOptions configures a Table.
type TableOptions[K comparable, V any] struct {
	// Shards is rounded up to a power of two. Default 64.
	Shards int
	// IdleTimeout evicts flows not touched for this long. Zero disables
	// idle eviction. Eviction only runs while Run is active.
	IdleTimeout time.Duration
	// OnClose is called exactly once for every flow that leaves the table,
	// whether by Remove, idle eviction or Close, after it is Closed. It runs
	// outside table locks.
	OnClose func(K, *Flow[V])
}

// Table maps keys (for example netip.AddrPort) to flows. It is safe for
// concurrent use; lookups lock one shard only.
type Table[K comparable, V any] struct {
	shards  []shard[K, V]
	mask    uint64
	seed    maphash.Seed
	idle    int64
	onClose func(K, *Flow[V])
	now     atomic.Int64 // coarse clock, advanced by Run
}

type shard[K comparable, V any] struct {
	mu sync.Mutex
	m  map[K]*Flow[V]
	_  [48]byte // keep neighbouring shard locks off one cache line
}

// NewTable returns an empty table.
func NewTable[K comparable, V any](o TableOptions[K, V]) *Table[K, V] {
	n := o.Shards
	if n <= 0 {
		n = 64
	}
	size := 1
	for size < n {
		size <<= 1
	}
	t := &Table[K, V]{
		shards: make([]shard[K, V], size), mask: uint64(size - 1),
		seed: maphash.MakeSeed(), idle: int64(o.IdleTimeout), onClose: o.OnClose,
	}
	for i := range t.shards {
		t.shards[i].m = make(map[K]*Flow[V])
	}
	t.now.Store(time.Now().UnixNano())
	return t
}

func (t *Table[K, V]) shard(k K) *shard[K, V] {
	return &t.shards[maphash.Comparable(t.seed, k)&t.mask]
}

// Get returns the flow for k and marks it as seen. The touch happens under
// the shard lock, so Sweep cannot evict a flow between lookup and touch.
// A returned flow can still be closed later by Remove, Sweep or Close;
// check State before relying on it.
func (t *Table[K, V]) Get(k K) (*Flow[V], bool) {
	s := t.shard(k)
	s.mu.Lock()
	f, ok := s.m[k]
	if ok {
		f.lastSeen.Store(t.now.Load())
	}
	s.mu.Unlock()
	return f, ok
}

// GetOrOpen returns the flow for k, creating it with newValue if absent.
// newValue runs under the shard lock and must be fast.
func (t *Table[K, V]) GetOrOpen(k K, newValue func() V) (f *Flow[V], created bool) {
	s := t.shard(k)
	s.mu.Lock()
	f, ok := s.m[k]
	if !ok {
		var v V
		if newValue != nil {
			v = newValue()
		}
		f = New(v)
		s.m[k] = f
	}
	f.lastSeen.Store(t.now.Load())
	s.mu.Unlock()
	return f, !ok
}

// Remove closes and removes the flow for k only if its ID is id, so a stale
// caller cannot remove a newer flow that reused the key. It reports whether
// a flow was removed.
func (t *Table[K, V]) Remove(k K, id uint64) bool {
	s := t.shard(k)
	s.mu.Lock()
	f, ok := s.m[k]
	if !ok || f.id != id {
		s.mu.Unlock()
		return false
	}
	delete(s.m, k)
	s.mu.Unlock()
	t.closed(k, f)
	return true
}

func (t *Table[K, V]) closed(k K, f *Flow[V]) {
	if f.Close() && t.onClose != nil {
		t.onClose(k, f)
	}
}

// Len returns the number of flows.
func (t *Table[K, V]) Len() int {
	n := 0
	for i := range t.shards {
		s := &t.shards[i]
		s.mu.Lock()
		n += len(s.m)
		s.mu.Unlock()
	}
	return n
}

// Range calls fn for every flow until fn returns false. fn runs under a
// shard lock and must not call back into the table.
func (t *Table[K, V]) Range(fn func(K, *Flow[V]) bool) {
	for i := range t.shards {
		s := &t.shards[i]
		s.mu.Lock()
		for k, f := range s.m {
			if !fn(k, f) {
				s.mu.Unlock()
				return
			}
		}
		s.mu.Unlock()
	}
}

// Sweep evicts flows idle longer than IdleTimeout as of now and returns how
// many it evicted. It also advances the table clock to now, so touches made
// after a manual Sweep are recorded at the new time; without that, a table
// swept by hand instead of by Run would see every flow as idle. Run calls
// Sweep periodically.
//
// Sweep visits every flow, holding one shard lock at a time. Cost grows
// with table size, and faster than linearly once the table outgrows the
// CPU caches: about 0.2ms for 10k flows and about 100ms for 1M flows
// (2 vCPU). With Run's 1s maximum tick, a million flows cost roughly 10%
// of one core. See BenchmarkSweepBySize.
func (t *Table[K, V]) Sweep(now time.Time) int {
	t.advance(now.UnixNano())
	if t.idle <= 0 {
		return 0
	}
	cutoff := now.UnixNano() - t.idle
	type victim struct {
		k K
		f *Flow[V]
	}
	var victims []victim
	for i := range t.shards {
		s := &t.shards[i]
		s.mu.Lock()
		for k, f := range s.m {
			if f.lastSeen.Load() < cutoff {
				delete(s.m, k)
				victims = append(victims, victim{k, f})
			}
		}
		s.mu.Unlock()
	}
	for _, v := range victims {
		t.closed(v.k, v.f)
	}
	return len(victims)
}

// Run advances the table's coarse clock and evicts idle flows until ctx is
// done, then closes every remaining flow. The clock resolution is a quarter
// of IdleTimeout, clamped to [10ms, 1s].
func (t *Table[K, V]) Run(ctx context.Context) error {
	tick := time.Second
	if t.idle > 0 {
		tick = time.Duration(t.idle / 4)
		tick = min(max(tick, 10*time.Millisecond), time.Second)
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Close()
			return nil
		case now := <-ticker.C:
			t.Sweep(now)
		}
	}
}

// advance moves the coarse clock forward, never back.
func (t *Table[K, V]) advance(ns int64) {
	for {
		cur := t.now.Load()
		if ns <= cur || t.now.CompareAndSwap(cur, ns) {
			return
		}
	}
}

// Close removes and closes every flow.
func (t *Table[K, V]) Close() {
	for i := range t.shards {
		s := &t.shards[i]
		s.mu.Lock()
		m := s.m
		s.m = make(map[K]*Flow[V])
		s.mu.Unlock()
		for k, f := range m {
			t.closed(k, f)
		}
	}
}
