// Package bufio reads and writes byte streams with memory proportional to
// the data in flight, not to the number of connections.
//
// Reader holds no buffer until it reads and gives it back when traffic is
// light. Writer sends a backlog of buffers in as few writes as the
// destination allows. Neither knows any protocol.
package bufio

import (
	"errors"
	"io"

	"github.com/ninepeach/go-netx/buf"
)

var (
	// ErrTooLarge means more than ReaderOptions.Max bytes would have to be
	// buffered, for example a frame longer than the protocol allows.
	ErrTooLarge = errors.New("bufio: data exceeds Max")
	// ErrNegativeCount is returned by Peek for a negative count.
	ErrNegativeCount = errors.New("bufio: negative count")
	// ErrReleased is returned when reading from a released Reader.
	ErrReleased = errors.New("bufio: reader released")
	// ErrInvalidCount means the source or destination reported a count
	// outside what it was offered.
	ErrInvalidCount = errors.New("bufio: invalid count from Read or Write")
)

const maxEmptyReads = 100

// ReaderOptions configures a Reader.
type ReaderOptions struct {
	IdleSize int // buffer for light traffic; default 512
	Max      int // most bytes ever buffered, the largest frame; default 64KiB
}

// Reader is a buffered reader whose buffer grows while data is pending and
// is given back once it is consumed. Slices it returns are borrowed: valid
// until the next call that reads, discards, shrinks, takes or releases.
// It is not safe for concurrent use.
type Reader struct {
	src       io.Reader
	b         *buf.Buf // nil until needed
	idle, max int
	last      int   // bytes the most recent read put in b
	empty     int   // consecutive (0, nil) reads
	err       error // sticky error from src
	done      bool
}

// NewReader returns a Reader over src. It panics if an option is negative
// or Max < IdleSize.
func NewReader(src io.Reader, o ReaderOptions) *Reader {
	if o.IdleSize == 0 {
		o.IdleSize = 512
	}
	if o.Max == 0 {
		o.Max = 64 << 10
	}
	if o.IdleSize < 0 || o.Max < o.IdleSize {
		panic("bufio: invalid ReaderOptions")
	}
	return &Reader{src: src, idle: o.IdleSize, max: o.Max}
}

// Len returns the number of buffered bytes.
func (r *Reader) Len() int {
	if r.b == nil {
		return 0
	}
	return r.b.Len()
}

// Buffered returns the data read and not yet discarded.
func (r *Reader) Buffered() []byte {
	if r.b == nil {
		return nil
	}
	return r.b.Bytes()
}

// Peek returns the next n bytes without consuming them, reading as needed.
// If it cannot get n bytes it returns what is buffered and the error:
// check both.
func (r *Reader) Peek(n int) ([]byte, error) {
	switch {
	case r.done:
		return nil, ErrReleased
	case n < 0:
		return nil, ErrNegativeCount
	case n > r.max:
		return r.Buffered(), ErrTooLarge
	}
	for r.Len() < n {
		if err := r.fill(n); err != nil {
			return r.Buffered(), err
		}
	}
	return r.Buffered()[:n:n], nil
}

// Fill reads from the source at most once, appending to the buffered data.
// It may add nothing. With Max bytes buffered it returns ErrTooLarge.
func (r *Reader) Fill() error {
	if r.done {
		return ErrReleased
	}
	if r.Len() == r.max {
		if r.err != nil {
			return r.err
		}
		return ErrTooLarge
	}
	return r.fill(r.Len() + 1)
}

// Discard consumes up to n buffered bytes and returns how many.
func (r *Reader) Discard(n int) int {
	if n = min(n, r.Len()); n <= 0 {
		return 0
	}
	r.b.Advance(n)
	if r.b.Len() == 0 {
		if r.b.Cap() > r.idle && r.last <= r.idle {
			r.drop()
		} else {
			r.b.Reset()
		}
	}
	return n
}

// Read implements io.Reader: buffered data first, then the source. With
// nothing buffered and len(p) >= IdleSize it reads straight into p.
func (r *Reader) Read(p []byte) (int, error) {
	if r.done {
		return 0, ErrReleased
	}
	if len(p) == 0 {
		return 0, nil
	}
	if r.Len() == 0 {
		if r.err != nil {
			return 0, r.err
		}
		if len(p) >= r.idle {
			for {
				n, err := r.src.Read(p)
				if n < 0 || n > len(p) {
					r.err = ErrInvalidCount
					return 0, r.err
				}
				if err = r.note(n, err); n > 0 {
					return n, nil // data first; a sticky error comes next time
				}
				if err != nil {
					return 0, err
				}
			}
		}
		for r.Len() == 0 {
			if err := r.fill(1); err != nil {
				return 0, err
			}
		}
	}
	return r.Discard(copy(p, r.Buffered())), nil
}

// Shrink gives the buffer back if nothing is buffered, and reports whether
// it did.
func (r *Reader) Shrink() bool {
	if r.b == nil || r.b.Len() != 0 {
		return false
	}
	r.drop()
	return true
}

// TakeBuffered hands the unread bytes to the caller as an owned buffer, or
// returns nil if there are none. It does not read; a sticky error stays
// with the Reader. The caller replays the bytes before reading the source.
func (r *Reader) TakeBuffered() *buf.Buf {
	if r.Len() == 0 {
		return nil
	}
	b := r.b
	r.b = nil
	return b
}

// Release gives the buffer back and ends the Reader. It is idempotent.
func (r *Reader) Release() {
	r.done = true
	r.src, r.err = nil, nil
	if r.b != nil {
		r.drop()
	}
}

func (r *Reader) drop() {
	r.b.Release()
	r.b = nil
}

// fill makes room for n buffered bytes and reads once. It returns nil when
// data arrived together with an error: the error is reported next time.
func (r *Reader) fill(n int) error {
	if r.err != nil {
		return r.err
	}
	if err := r.reserve(n); err != nil {
		return err
	}
	p := r.b.FreeBytes()
	k, err := r.src.Read(p)
	if k < 0 || k > len(p) {
		r.err = ErrInvalidCount
		return r.err
	}
	r.b.Extend(k)
	r.last = k
	if err = r.note(k, err); k > 0 {
		return nil
	}
	return err
}

// note records one read from src and returns the error it ends in. Only
// consecutive (0, nil) reads count toward io.ErrNoProgress: data, an error,
// or reporting io.ErrNoProgress itself starts the count again.
func (r *Reader) note(n int, err error) error {
	if n == 0 && err == nil {
		if r.empty++; r.empty < maxEmptyReads {
			return nil
		}
		r.err = io.ErrNoProgress
		return r.err
	}
	r.empty = 0
	if err != nil {
		r.err = err
	}
	return err
}

// reserve leaves free space in a buffer able to hold n bytes: it
// allocates, compacts, or moves to a larger class, never past max.
func (r *Reader) reserve(n int) error {
	if n > r.max {
		return ErrTooLarge
	}
	b := r.b
	if b == nil {
		r.b = buf.Get(min(max(r.idle, buf.Capacity(n)), r.max))
		return nil
	}
	free := len(b.FreeBytes())
	used := b.Cap() - b.Len() - free // consumed bytes in front of the data
	if free > 0 && b.Cap()-used >= n {
		return nil
	}
	if used > 0 && b.Cap() >= n {
		p := b.Bytes()
		b.Reset()
		copy(b.Extend(len(p)), p) // overlapping move to the front
		return nil
	}
	size := min(buf.Capacity(max(n, b.Cap()+1)), r.max)
	if size <= b.Cap() {
		return ErrTooLarge
	}
	nb := buf.Get(size)
	copy(nb.Extend(b.Len()), b.Bytes())
	b.Release()
	r.b = nb
	return nil
}
