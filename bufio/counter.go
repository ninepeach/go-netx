package bufio

import (
	"io"
	"net"
	"sync"
	"sync/atomic"

	"github.com/ninepeach/go-netx/buf"
)

// CounterConn counts bytes accepted by the underlying connection, including
// positive counts returned with an error.
type CounterConn struct {
	net.Conn
	read, written atomic.Int64
	once          sync.Once
	closeErr      error
}

func NewCounterConn(c net.Conn) *CounterConn { return &CounterConn{Conn: c} }

func (c *CounterConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n < 0 || n > len(p) {
		return 0, ErrInvalidCount
	}
	c.read.Add(int64(n))
	return n, err
}

func (c *CounterConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n < 0 || n > len(p) {
		return 0, ErrInvalidCount
	}
	c.written.Add(int64(n))
	if err == nil && n < len(p) {
		err = io.ErrShortWrite
	}
	return n, err
}

// WriteBuffer writes b without consuming it or retaining it.
func (c *CounterConn) WriteBuffer(b *buf.Buf) (int, error) { return c.Write(b.Bytes()) }

// ReadBuffer reads into the free tail of b.
func (c *CounterConn) ReadBuffer(b *buf.Buf) (int, error) { return b.ReadOnceFrom(c) }

// WriteVectorised writes vec through the connection's own vectorised path
// and counts what it accepted, so a counter does not cost a batch its
// single system call.
func (c *CounterConn) WriteVectorised(vec [][]byte) (int64, error) {
	n, err := WriteVectorised(c.Conn, vec)
	if n > 0 {
		c.written.Add(n)
	}
	return n, err
}

// UnwrapCountReader returns the connection beneath the counter and the
// counter that bytes read from it must be added to.
func (c *CounterConn) UnwrapCountReader() (io.Reader, Counter) { return c.Conn, &c.read }

// UnwrapCountWriter is UnwrapCountReader for the write direction.
func (c *CounterConn) UnwrapCountWriter() (io.Writer, Counter) { return c.Conn, &c.written }

// Counts returns the total bytes read and written.
func (c *CounterConn) Counts() (read, written int64) { return c.read.Load(), c.written.Load() }

func (c *CounterConn) Close() error {
	c.once.Do(func() { c.closeErr = c.Conn.Close() })
	return c.closeErr
}

func (c *CounterConn) CloseWrite() error { return closeWrite(c.Conn) }
func (c *CounterConn) CloseRead() error  { return closeRead(c.Conn) }
