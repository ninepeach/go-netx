package bufio

import "io"

// Counter accumulates bytes moved. *atomic.Int64 is one.
type Counter interface{ Add(n int64) int64 }

// CountedReader is a wrapper whose only effect on reads is counting them.
// Reading the returned reader directly and adding the bytes to the returned
// counter is equivalent to reading through the wrapper. A wrapper that
// transforms, limits or buffers data must not implement it.
type CountedReader interface {
	UnwrapCountReader() (io.Reader, Counter)
}

// CountedWriter is CountedReader for the write direction.
type CountedWriter interface {
	UnwrapCountWriter() (io.Writer, Counter)
}

// ReaderUnwrapper is a wrapper that adds nothing to reads: reading the
// returned reader directly cannot be told apart from reading through the
// wrapper. A connection type that only carries identity or bookkeeping
// implements it so that it does not hide the socket's fast paths. Its
// signature uses only io types, so no import of this package is needed.
// The caller passes the outer reader. Return nil if it is not the receiver
// itself, including when the method was promoted by embedding. TLS, limiters,
// counters and anything that buffers must not implement this capability.
type ReaderUnwrapper interface {
	UnwrapReader(outer io.Reader) io.Reader
}

// WriterUnwrapper is ReaderUnwrapper for the write direction.
type WriterUnwrapper interface {
	UnwrapWriter(outer io.Writer) io.Writer
}

// UnwrapCountReader peels every pass-through and counting wrapper off r.
// It returns the reader beneath them and the counters met on the way,
// outermost first; with no such wrapper it returns r and nil. A wrapper
// that declares neither capability is never unwrapped.
func UnwrapCountReader(r io.Reader) (io.Reader, []Counter) {
	var counters []Counter
	for {
		switch c := r.(type) {
		case CountedReader:
			var counter Counter
			r, counter = c.UnwrapCountReader()
			counters = append(counters, counter)
		case ReaderUnwrapper:
			next := c.UnwrapReader(r)
			if next == nil {
				return r, counters
			}
			r = next
		default:
			return r, counters
		}
	}
}

// UnwrapCountWriter is UnwrapCountReader for the write direction.
func UnwrapCountWriter(w io.Writer) (io.Writer, []Counter) {
	var counters []Counter
	for {
		switch c := w.(type) {
		case CountedWriter:
			var counter Counter
			w, counter = c.UnwrapCountWriter()
			counters = append(counters, counter)
		case WriterUnwrapper:
			next := c.UnwrapWriter(w)
			if next == nil {
				return w, counters
			}
			w = next
		default:
			return w, counters
		}
	}
}

// vectorTarget steps through pass-through wrappers until it reaches a
// writer with its own vectorised path or one that cannot be unwrapped.
// Counters are left in place: they take the vector themselves.
func vectorTarget(w io.Writer) io.Writer {
	for {
		if _, ok := w.(VectorisedWriter); ok {
			return w
		}
		u, ok := w.(WriterUnwrapper)
		if !ok {
			return w
		}
		next := u.UnwrapWriter(w)
		if next == nil {
			return w
		}
		w = next
	}
}
