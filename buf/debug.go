//go:build netxdebug

package buf

import "sync/atomic"

// Debug reports whether ownership checks are compiled in.
const Debug = true

var outstanding atomic.Int64

type debugState struct{ live bool }

func (b *Buf) onGet() {
	b.live = true
	outstanding.Add(1)
}

func (b *Buf) onRelease() {
	if !b.live {
		panic("buf: Release of a buffer that is not owned (double Release?)")
	}
	b.live = false
	outstanding.Add(-1)
}

// Outstanding returns the number of buffers obtained and not yet released.
// It is only meaningful with -tags netxdebug.
func Outstanding() int64 { return outstanding.Load() }
