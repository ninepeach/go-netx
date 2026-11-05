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
// Flow is not a session. Authentication, user identity and reconnection
// across flows belong to the gateway.
package flow

import (
	"sync/atomic"
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
	id       uint64
	state    atomic.Uint32
	lastSeen atomic.Int64 // unix nanoseconds, coarse
	Value    V
}

// New returns an open flow with a fresh ID.
func New[V any](value V) *Flow[V] {
	return &Flow[V]{id: NextID(), Value: value}
}

func (f *Flow[V]) ID() uint64   { return f.id }
func (f *Flow[V]) State() State { return State(f.state.Load()) }

// BeginClose moves Open → Closing and reports whether this call did it.
func (f *Flow[V]) BeginClose() bool {
	return f.state.CompareAndSwap(uint32(Open), uint32(Closing))
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
			return true
		}
	}
}

// LastSeen returns the coarse unix-nanosecond time of the last Touch.
func (f *Flow[V]) LastSeen() int64 { return f.lastSeen.Load() }
