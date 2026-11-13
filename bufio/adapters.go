package bufio

import (
	"errors"
	"io"
	"net"
	"sync"

	"github.com/ninepeach/go-netx/buf"
)

// CachedReader owns a pre-read prefix and borrows its source. Reads are
// serialized so the prefix always precedes bytes read from the source.
// Close waits for an in-flight Read; it does not close the borrowed source.
type CachedReader struct {
	source io.Reader
	mu     sync.Mutex
	prefix *buf.Buf
	closed bool
}

// NewCachedReader takes ownership of prefix. Close releases unread prefix
// bytes, and the source remains the caller's responsibility.
func NewCachedReader(src io.Reader, prefix *buf.Buf) *CachedReader {
	return &CachedReader{source: src, prefix: prefix}
}

func (r *CachedReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return 0, ErrReleased
	}
	if r.prefix != nil && r.prefix.Len() > 0 {
		n, err := r.prefix.Read(p)
		if r.prefix.Len() == 0 {
			r.releasePrefix()
		}
		r.mu.Unlock()
		return n, err
	}
	r.releasePrefix()
	defer r.mu.Unlock()
	return r.source.Read(p)
}

func (r *CachedReader) releasePrefix() {
	if r.prefix != nil {
		r.prefix.Release()
		r.prefix = nil
	}
}

func (r *CachedReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	r.source = nil
	r.releasePrefix()
	return nil
}

// ComposeConn replaces either I/O direction while retaining the original
// connection's lifetime and addresses. A nil direction uses c. Replacements
// must honor c's deadlines and close if used with a cancelling transfer.
func ComposeConn(c net.Conn, r io.Reader, w io.Writer) net.Conn {
	return &composedConn{Conn: c, reader: r, writer: w}
}

type composedConn struct {
	net.Conn
	reader io.Reader
	writer io.Writer
}

func (c *composedConn) Read(p []byte) (int, error) {
	if c.reader != nil {
		return c.reader.Read(p)
	}
	return c.Conn.Read(p)
}
func (c *composedConn) Write(p []byte) (int, error) {
	if c.writer != nil {
		return c.writer.Write(p)
	}
	return c.Conn.Write(p)
}
func (c *composedConn) CloseWrite() error {
	if c.writer == nil {
		return closeWrite(c.Conn)
	}
	if w, ok := c.writer.(interface{ CloseWrite() error }); ok {
		return w.CloseWrite()
	}
	return errors.ErrUnsupported
}
func (c *composedConn) CloseRead() error {
	if c.reader == nil {
		return closeRead(c.Conn)
	}
	if r, ok := c.reader.(interface{ CloseRead() error }); ok {
		return r.CloseRead()
	}
	return errors.ErrUnsupported
}

// WithAddresses projects caller-provided addresses while preserving I/O,
// deadlines and half-close. Nil addresses use those of c.
func WithAddresses(c net.Conn, local, remote net.Addr) net.Conn {
	return &addressConn{Conn: c, local: local, remote: remote}
}

type addressConn struct {
	net.Conn
	local, remote net.Addr
}

func (c *addressConn) LocalAddr() net.Addr {
	if c.local != nil {
		return c.local
	}
	return c.Conn.LocalAddr()
}
func (c *addressConn) RemoteAddr() net.Addr {
	if c.remote != nil {
		return c.remote
	}
	return c.Conn.RemoteAddr()
}
func (c *addressConn) CloseWrite() error { return closeWrite(c.Conn) }
func (c *addressConn) CloseRead() error  { return closeRead(c.Conn) }
