//go:build !netxdebug

package buf

// Debug reports whether ownership checks are compiled in.
const Debug = false

type debugState struct{}

func (b *Buf) onGet()     {}
func (b *Buf) onRelease() {}

// Outstanding returns the number of buffers obtained and not yet released.
// It is only meaningful with -tags netxdebug and returns 0 otherwise.
func Outstanding() int64 { return 0 }
