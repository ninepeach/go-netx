package bufio

import (
	"errors"
	"io"
	"net"
	"sync"

	"github.com/ninepeach/go-netx/buf"
)

// ExtendedReader can append bytes directly to a borrowed buffer.
type ExtendedReader interface {
	io.Reader
	ReadBuffer(*buf.Buf) (int, error)
}

// ExtendedWriter can write a borrowed buffer without modifying or retaining it.
type ExtendedWriter interface {
	io.Writer
	WriteBuffer(*buf.Buf) (int, error)
}

// ExtendedConn combines the stream capabilities with net.Conn.
type ExtendedConn interface {
	net.Conn
	ExtendedReader
	ExtendedWriter
}

// WriteBuffer writes a borrowed buffer through an advertised capability or
// ordinary Write. It validates the count and does not consume the buffer.
func WriteBuffer(w io.Writer, b *buf.Buf) (int, error) {
	size := b.Len()
	var n int
	var err error
	if extended, ok := w.(ExtendedWriter); ok {
		n, err = extended.WriteBuffer(b)
	} else {
		n, err = w.Write(b.Bytes())
	}
	if n < 0 || n > size {
		return 0, ErrInvalidCount
	}
	if err == nil && n != size {
		err = io.ErrShortWrite
	}
	return n, err
}

// ReadBuffer appends data to b, preserving the reader's buffered data order.
func (r *Reader) ReadBuffer(b *buf.Buf) (int, error) {
	if r.done {
		return 0, ErrReleased
	}
	return b.ReadOnceFrom(r)
}

// ReadWaiter may wait for readability before allocating a buffer. Its
// caller owns any returned buffer, including when an error accompanies data.
type ReadWaiter interface {
	io.Reader
	WaitReadBuffer(size int) (*buf.Buf, error)
}

// ReadBuffer reads one chunk, using an explicit ReadWaiter capability if
// available. It returns an owned buffer only when data was read.
func ReadBuffer(r io.Reader, size int) (*buf.Buf, error) {
	if size <= 0 {
		panic("bufio: invalid read size")
	}
	if waiter, ok := r.(ReadWaiter); ok {
		b, err := waiter.WaitReadBuffer(size)
		if b != nil && b.Len() > size {
			b.Release()
			return nil, ErrInvalidCount
		}
		if b != nil && b.Len() == 0 {
			b.Release()
			return nil, err
		}
		return b, err
	}
	b := buf.Get(size)
	var n int
	var err error
	if extended, ok := r.(ExtendedReader); ok {
		n, err = extended.ReadBuffer(b)
		if n < 0 || n > size || n != b.Len() {
			err = ErrInvalidCount
			n = 0
		}
	} else {
		n, err = r.Read(b.FreeBytes())
		if n < 0 || n > size {
			b.Release()
			return nil, ErrInvalidCount
		}
		b.Extend(n)
	}
	if n <= 0 {
		b.Release()
		return nil, err
	}
	return b, err
}

// CachedConn replays an owned prefix before reading Conn. Its read and
// write directions may be used independently.
type CachedConn struct {
	net.Conn
	mu       sync.Mutex
	readMu   sync.Mutex // serializes Read, ReadBuffer and WriteTo
	prefix   *buf.Buf
	closed   bool
	once     sync.Once
	closeErr error
}

// NewCachedConn transfers ownership of prefix, which may be nil.
func NewCachedConn(c net.Conn, prefix *buf.Buf) *CachedConn {
	return &CachedConn{Conn: c, prefix: prefix}
}

func (c *CachedConn) Read(p []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, net.ErrClosed
	}
	if c.prefix != nil {
		if c.prefix.Len() > 0 {
			n, err := c.prefix.Read(p)
			if c.prefix.Len() == 0 {
				c.dropPrefix()
			}
			c.mu.Unlock()
			return n, err
		}
		c.dropPrefix()
	}
	c.mu.Unlock()
	return c.Conn.Read(p)
}

func (c *CachedConn) dropPrefix() {
	if c.prefix != nil {
		c.prefix.Release()
		c.prefix = nil
	}
}

// Close releases the prefix and closes the underlying connection once.
func (c *CachedConn) Close() error {
	c.once.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.dropPrefix()
		c.mu.Unlock()
		c.closeErr = c.Conn.Close()
	})
	return c.closeErr
}

// CloseWrite forwards a write half-close.
func (c *CachedConn) CloseWrite() error { return closeWrite(c.Conn) }

// CloseRead closes the underlying read direction and drops the prefix on success.
func (c *CachedConn) CloseRead() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	err := closeRead(c.Conn)
	if err == nil {
		c.dropPrefix()
	}
	return err
}

// ReadBuffer appends bytes while preserving prefix order.
func (c *CachedConn) ReadBuffer(b *buf.Buf) (int, error) {
	return b.ReadOnceFrom(c)
}

// WriteBuffer writes borrowed data through the underlying write direction.
func (c *CachedConn) WriteBuffer(b *buf.Buf) (int, error) {
	return WriteBuffer(c.Conn, b)
}

func closeWrite(c net.Conn) error {
	if half, ok := c.(interface{ CloseWrite() error }); ok {
		return half.CloseWrite()
	}
	return errors.ErrUnsupported
}

func closeRead(c net.Conn) error {
	if half, ok := c.(interface{ CloseRead() error }); ok {
		return half.CloseRead()
	}
	return errors.ErrUnsupported
}

// WriteTo forwards the prefix before copying from the connection. Serialize
// it with other reads. A failed prefix write releases its unsent remainder.
func (c *CachedConn) WriteTo(dst io.Writer) (int64, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, net.ErrClosed
	}
	prefix := c.prefix
	c.prefix = nil
	c.mu.Unlock()
	var n int64
	if prefix != nil {
		written, err := func() (int64, error) {
			defer prefix.Release()
			return prefix.WriteTo(dst)
		}()
		n = written
		if err != nil {
			return n, err
		}
	}
	written, err := io.Copy(dst, c.Conn)
	return n + written, err
}
