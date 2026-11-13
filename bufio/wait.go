package bufio

import (
	"io"
	"net"
)

// NewReadWaiter returns a readiness adapter for a supported bare stream
// connection or an explicitly provided capability. Serialize it with reads.
func NewReadWaiter(r io.Reader) (ReadWaiter, bool) {
	if w, ok := r.(ReadWaiter); ok {
		return w, true
	}
	c, ok := r.(*net.TCPConn)
	if !ok {
		return nil, false
	}
	return tcpReadWaiter(c)
}
