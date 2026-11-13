package bufio

import (
	"io"
	"net"
	"sync"

	"github.com/ninepeach/go-netx/buf"
)

// Writer collects buffers and submits them as one batch. It is not a
// queue: Flush and Discard release every added buffer whether or not it
// was written, and nothing is kept for retry. It is not safe for
// concurrent use.
type Writer struct {
	dst  io.Writer
	own  []*buf.Buf  // buffers to release
	vec  net.Buffers // everything to send, in order
	tmp  net.Buffers // the view WriteTo consumes; a field so it does not escape
	size int
}

// NewWriter returns a Writer that sends to dst.
func NewWriter(dst io.Writer) *Writer { return &Writer{dst: dst} }

// Add queues b and takes ownership of it.
func (w *Writer) Add(b *buf.Buf) {
	w.own = append(w.own, b)
	w.AddShared(b.Bytes())
}

// AddShared queues p without taking ownership. p must not change until
// Flush or Discard returns; after that the Writer holds no reference to it.
func (w *Writer) AddShared(p []byte) {
	if len(p) == 0 {
		return
	}
	w.vec = append(w.vec, p)
	w.size += len(p)
}

// Pending returns the number of queued segments and bytes.
func (w *Writer) Pending() (buffers, bytes int) { return len(w.vec), w.size }

// Flush writes the queue in order and returns the bytes the destination
// acknowledged. Unless that is every byte, the error is non-nil:
// io.ErrShortWrite if the destination gave none. The queue is empty
// afterwards either way.
func (w *Writer) Flush() (int64, error) {
	var n int64
	var err error
	if len(w.vec) > 1 {
		target := vectorTarget(w.dst)
		if dst, ok := target.(VectorisedWriter); ok {
			n, err = dst.WriteVectorised(w.vec)
		} else if streamVector(target) {
			w.tmp = w.vec
			n, err = w.tmp.WriteTo(target)
		} else {
			n, err = writeSegments(w.dst, w.vec)
		}
	} else {
		n, err = writeSegments(w.dst, w.vec)
	}
	size := int64(w.size)
	w.Discard()
	switch {
	case n < 0 || n > size:
		return 0, ErrInvalidCount
	case err == nil && n < size:
		err = io.ErrShortWrite
	}
	return n, err
}

// Discard drops the queue without writing. The Writer can be used again.
func (w *Writer) Discard() {
	for i, b := range w.own {
		b.Release()
		w.own[i] = nil
	}
	clear(w.vec)
	w.own, w.vec, w.tmp, w.size = w.own[:0], w.vec[:0], nil, 0
}

// VectorisedWriter writes a borrowed vector in order, returning the number
// of bytes accepted. The vector and its contents may not be changed or
// retained. A partial write must return an error.
type VectorisedWriter interface {
	WriteVectorised([][]byte) (int64, error)
}

// WriteVectorised writes vec to w in order with the best path w offers: its
// own VectorisedWriter, writev on a stream connection that is bare or
// behind pass-through wrappers, or one checked Write per segment. It does not change or retain vec. A partial write
// returns an error with the bytes accepted.
func WriteVectorised(w io.Writer, vec [][]byte) (int64, error) {
	if len(vec) > 1 {
		w = vectorTarget(w)
		if v, ok := w.(VectorisedWriter); ok {
			n, err := v.WriteVectorised(vec)
			var size int64
			for _, p := range vec {
				size += int64(len(p))
			}
			switch {
			case n < 0 || n > size:
				return 0, ErrInvalidCount
			case err == nil && n < size:
				err = io.ErrShortWrite
			}
			return n, err
		}
		if streamVector(w) {
			// net.Buffers.WriteTo consumes its receiver; give it a copy.
			scratch := vectorPool.Get().(*[][]byte)
			all := append((*scratch)[:0], vec...)
			view := net.Buffers(all)
			n, err := view.WriteTo(w)
			clear(all)
			*scratch = all[:0]
			vectorPool.Put(scratch)
			return n, err
		}
	}
	return writeSegments(w, vec)
}

var vectorPool = sync.Pool{New: func() any { return new([][]byte) }}

func streamVector(w io.Writer) bool {
	switch c := w.(type) {
	case *net.TCPConn:
		return true
	case *net.UnixConn:
		return c.LocalAddr().Network() == "unix"
	default:
		return false
	}
}

func writeSegments(w io.Writer, segments [][]byte) (int64, error) {
	var total int64
	for _, p := range segments {
		n, err := w.Write(p)
		if n < 0 || n > len(p) {
			return -1, ErrInvalidCount
		}
		total += int64(n)
		if err != nil {
			return total, err
		}
		if n != len(p) {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}
