// Package flow gives transport-level identity to a connection or datagram
// stream: a unique ID, a close state machine, and an idle-expiring table.
//
// Flow does no I/O. Transports (tcp, udp) move bytes; gateways combine them
// with flow, for example by looking up udp.Packet.From in a Table.
//
// IDs are process-wide and strictly increasing, so an ID doubles as an
// epoch: a stale callback that remembers (key, id) can tell a reused key
// belongs to a newer flow because the IDs differ.
//
// Lookups (Table.Get, GetOrOpen on an existing key) never change a flow's
// LastSeen; Flow.Touch is the only way to record activity, and idle expiry
// uses LastSeen. A gateway touches a flow when the transport delivers real
// data for it, for example:
//
//	f, _ := flows.GetOrOpen(p.From, newPeer) // in a udp.Handler
//	f.Touch()                                 // a datagram arrived
//
// Background queries and management code must not touch.
//
// Flow is not a session. Authentication, user identity and reconnection
// across flows belong to the gateway.
package flow

import (
	"sync/atomic"
	"time"
)

var lastID atomic.Uint64

// NextID returns a new process-wide unique, strictly increasing ID.
// IDs start at 1; 0 never identifies a flow.
func NextID() uint64 { return lastID.Add(1) }

// State is a flow's position in Open → Closing → Closed.
type State uint32

const (
	Open State = iota
	Closing
	Closed
)

func (s State) String() string {
	switch s {
	case Open:
		return "open"
	case Closing:
		return "closing"
	case Closed:
		return "closed"
	}
	return "unknown"
}

// Flow is one transport-level flow carrying a gateway-defined value.
type Flow[V any] struct {
	id    uint64
	state atomic.Uint32
	// seen holds LastSeen in unix nanoseconds with the lowest bit used as
	// the frozen flag: once set (BeginClose, Close, leaving a Table), Touch
	// no longer changes it. Keeping both in one word lets Touch and Sweep
	// decide with one CAS who came first.
	seen  atomic.Int64
	clock *atomic.Int64 // the owning Table's coarse clock; nil: time.Now
	Value V
}

// frozen is the bit of Flow.seen that stops Touch.
const frozen = 1

// New returns an open flow with a fresh ID. LastSeen starts at the current
// time. Flows created by Table.GetOrOpen use the table's clock instead.
func New[V any](value V) *Flow[V] {
	f := &Flow[V]{id: NextID(), Value: value}
	f.seen.Store(time.Now().UnixNano() &^ frozen)
	return f
}

func newInTable[V any](value V, clock *atomic.Int64) *Flow[V] {
	f := &Flow[V]{id: NextID(), Value: value, clock: clock}
	f.seen.Store(clock.Load() &^ frozen)
	return f
}

func (f *Flow[V]) ID() uint64   { return f.id }
func (f *Flow[V]) State() State { return State(f.state.Load()) }

// BeginClose moves Open → Closing and reports whether this call did it.
// After it returns, Touch has no effect.
func (f *Flow[V]) BeginClose() bool {
	if !f.state.CompareAndSwap(uint32(Open), uint32(Closing)) {
		return false
	}
	f.freeze()
	return true
}

// Close moves the flow to Closed and reports whether this call did it.
// Exactly one caller ever gets true, so it is safe to release resources on
// true.
func (f *Flow[V]) Close() bool {
	for {
		s := f.state.Load()
		if State(s) == Closed {
			return false
		}
		if f.state.CompareAndSwap(s, uint32(Closed)) {
			f.freeze()
			return true
		}
	}
}

// Touch records activity on the flow: it sets LastSeen to the current time
// (the owning Table's coarse clock, or time.Now for a flow made by New).
// Call it when the transport delivers real data for the flow; lookups,
// background queries and management operations must not call it, so that
// idle expiry reflects traffic only.
//
// Touch is safe for concurrent use and never changes State. It only has
// an effect on an Open flow still in its table: after BeginClose, Close,
// Remove, or eviction by Sweep it does nothing, so a flow that is leaving
// cannot be revived. LastSeen never moves backwards.
func (f *Flow[V]) Touch() {
	var now int64
	if f.clock != nil {
		now = f.clock.Load()
	} else {
		now = time.Now().UnixNano()
	}
	now &^= frozen
	for {
		v := f.seen.Load()
		if v&frozen != 0 || now <= v {
			return // frozen, or nothing newer to record (the common case)
		}
		if f.seen.CompareAndSwap(v, now) {
			return
		}
	}
}

// LastSeen returns the coarse unix-nanosecond time of the last activity
// recorded by Touch, or of the flow's creation if it was never touched.
// Lookups do not change it.
func (f *Flow[V]) LastSeen() int64 { return f.seen.Load() &^ frozen }

func (f *Flow[V]) freeze() { f.seen.Or(frozen) }

// expire freezes the flow if it has been idle since before cutoff and
// reports whether it did, or whether it was frozen already. A Touch that
// lands first makes the CAS fail, so a flow touched in time is kept.
func (f *Flow[V]) expire(cutoff int64) bool {
	v := f.seen.Load()
	if v&^frozen >= cutoff {
		return false
	}
	return v&frozen != 0 || f.seen.CompareAndSwap(v, v|frozen)
}
