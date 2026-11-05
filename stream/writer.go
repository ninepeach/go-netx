package stream

import (
	"io"
	"net"

	"github.com/ninepeach/go-netx/buf"
)

// BuffersWriter is implemented by connections that can send several
// buffers with one system call. *tcp.Conn implements it; so do wrappers
// that want Writer to keep using writev. Go's own net.Buffers only uses
// writev on the standard library's connection types, so a wrapper that
// does not implement this silently falls back to one Write per buffer.
type BuffersWriter interface {
	WriteBuffers(*net.Buffers) (int64, error)
}

// Writer collects owned buffers and sends them in one writev. It is not
// safe for concurrent use; one goroutine owns a connection's Writer.
//
// Ownership: Add takes ownership of the buffer. Flush and Discard release
// every added buffer, whether or not it was written: after a failed write
// the stream is broken and the data is of no further use.
type Writer struct {
	dst  io.Writer
	held []*buf.Buf
	vec  net.Buffers // backing array reused across flushes
	tmp  net.Buffers // the view WriteTo consumes; a field so it does not escape per call
	size int
}

// NewWriter returns a Writer that sends to dst.
func NewWriter(dst io.Writer) *Writer { return &Writer{dst: dst} }

// Add queues b for the next Flush and takes ownership of it.
func (w *Writer) Add(b *buf.Buf) {
	w.held = append(w.held, b)
	w.size += len(b.B)
}

// Pending returns how many buffers and bytes are queued.
func (w *Writer) Pending() (buffers, bytes int) { return len(w.held), w.size }

// Flush writes every queued buffer in order, then releases them all. A
// single buffer is written with Write (writev only pays off for several).
func (w *Writer) Flush() (int64, error) {
	var n int64
	var err error
	switch len(w.held) {
	case 0:
		return 0, nil
	case 1:
		var k int
		k, err = w.dst.Write(w.held[0].B)
		n = int64(k)
	default:
		w.vec = w.vec[:0]
		for _, b := range w.held {
			w.vec = append(w.vec, b.B)
		}
		w.tmp = w.vec
		if bw, ok := w.dst.(BuffersWriter); ok {
			n, err = bw.WriteBuffers(&w.tmp)
		} else {
			n, err = w.tmp.WriteTo(w.dst)
		}
		clear(w.vec) // drop references to released memory
	}
	w.Discard()
	return n, err
}

// Discard releases every queued buffer without writing it.
func (w *Writer) Discard() {
	for i, b := range w.held {
		b.Release()
		w.held[i] = nil
	}
	w.held = w.held[:0]
	w.size = 0
}
