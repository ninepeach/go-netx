//go:build linux || darwin

package bufio

import (
	"io"
	"net"
	"os"
	"syscall"

	"github.com/ninepeach/go-netx/buf"
)

// tcpWaiter reads a TCP connection through its descriptor so that a buffer
// is held only while there are bytes to put in it: an idle connection
// blocked in WaitReadBuffer owns no memory.
type tcpWaiter struct {
	c   *net.TCPConn
	raw syscall.RawConn

	// State of one WaitReadBuffer call, kept here so that the callback is
	// allocated once. Calls are serialized by the caller.
	size int
	b    *buf.Buf
	n    int
	err  error
	read func(fd uintptr) bool
}

func tcpReadWaiter(c *net.TCPConn) (ReadWaiter, bool) {
	raw, err := c.SyscallConn()
	if err != nil {
		return nil, false
	}
	w := &tcpWaiter{c: c, raw: raw}
	w.read = w.tryRead
	return w, true
}

func (w *tcpWaiter) Read(p []byte) (int, error) { return w.c.Read(p) }

// tryRead runs with the descriptor locked by the runtime poller. It reports
// false to wait for readability and be called again.
func (w *tcpWaiter) tryRead(fd uintptr) bool {
	b := buf.Get(w.size)
	for {
		w.n, w.err = syscall.Read(int(fd), b.FreeBytes())
		if w.err != syscall.EINTR {
			break
		}
	}
	if w.err != nil || w.n <= 0 {
		b.Release()
		return w.err != syscall.EAGAIN
	}
	w.b = b
	return true
}

func (w *tcpWaiter) WaitReadBuffer(size int) (*buf.Buf, error) {
	if size <= 0 {
		panic("bufio: invalid read size")
	}
	w.size, w.b, w.n, w.err = size, nil, 0, nil
	if err := w.raw.Read(w.read); err != nil {
		return nil, err // deadline, or the connection was closed
	}
	if w.err != nil {
		return nil, &net.OpError{Op: "read", Net: "tcp", Source: w.c.LocalAddr(), Addr: w.c.RemoteAddr(), Err: os.NewSyscallError("read", w.err)}
	}
	b := w.b
	w.b = nil
	if b == nil {
		return nil, io.EOF
	}
	if w.n > size {
		b.Release()
		return nil, ErrInvalidCount
	}
	b.Extend(w.n)
	return b, nil
}
