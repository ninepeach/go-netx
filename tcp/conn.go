package tcp

import (
	"errors"
	"fmt"
	"io"
)

// CloseWrite forwards a TCP write half-close.
func (c *Conn) CloseWrite() error {
	if closer, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return closer.CloseWrite()
	}
	return fmt.Errorf("tcp: CloseWrite: %w", errors.ErrUnsupported)
}

// CloseRead forwards a TCP read half-close.
func (c *Conn) CloseRead() error {
	if closer, ok := c.Conn.(interface{ CloseRead() error }); ok {
		return closer.CloseRead()
	}
	return fmt.Errorf("tcp: CloseRead: %w", errors.ErrUnsupported)
}

// ReadFrom preserves the underlying connection's optimized copy path.
func (c *Conn) ReadFrom(src io.Reader) (int64, error) {
	if other, ok := src.(*Conn); ok {
		src = other.Conn
	}
	if reader, ok := c.Conn.(io.ReaderFrom); ok {
		return reader.ReadFrom(src)
	}
	return io.Copy(writerOnly{c.Conn}, src)
}

// WriteTo preserves the underlying connection's optimized copy path.
func (c *Conn) WriteTo(dst io.Writer) (int64, error) {
	if other, ok := dst.(*Conn); ok {
		dst = other.Conn
	}
	if writer, ok := c.Conn.(io.WriterTo); ok {
		return writer.WriteTo(dst)
	}
	return io.Copy(dst, readerOnly{c.Conn})
}

// UnwrapReader and UnwrapWriter declare that Conn adds nothing to I/O, so
// that vectorised writes, read-wait and kernel copies reach the socket.
// Identity, admission and tracking stay with Conn; only Read and Write are
// equivalent on the returned connection.
func (c *Conn) UnwrapReader(outer io.Reader) io.Reader {
	if outer != c {
		return nil
	}
	return c.Conn
}

// UnwrapWriter is UnwrapReader for the write direction.
func (c *Conn) UnwrapWriter(outer io.Writer) io.Writer {
	if outer != c {
		return nil
	}
	return c.Conn
}

type writerOnly struct{ io.Writer }
type readerOnly struct{ io.Reader }
