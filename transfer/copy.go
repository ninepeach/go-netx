// Package transfer copies streams synchronously and coordinates duplex I/O.
package transfer

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/ninepeach/go-netx/buf"
	"github.com/ninepeach/go-netx/bufio"
)

const copySize = 32 << 10
const maxEmptyReads = 100

// Copy copies until EOF. It borrows both endpoints and uses only explicitly
// advertised capabilities. A failed optimized transfer is never replayed.
//
// Counting wrappers are stepped around and credited with exactly the bytes
// moved, read and written separately, so they do not cost the fast paths
// beneath them. Pass-through wrappers can explicitly expose their endpoints.
// If a wrapper declines, the session uses its ordinary I/O and avoids
// WriterTo and ReaderFrom methods that embedding may have promoted.
// Otherwise the path is the first that applies:
//
//  1. Counted stream sockets on Linux: splice, counting as it goes.
//  2. Uncounted endpoints with their own fast path: the source's WriteTo, or
//     for a bare TCP source the standard library's, when that is zero-copy.
//  3. A bare TCP source, or any endpoint with a buffer or read-wait
//     capability, or any counted endpoint: one pooled chunk at a time, with
//     no buffer held while a TCP source is idle.
//  4. The destination's ReadFrom, then a plain buffered loop.
func Copy(dst io.Writer, src io.Reader) (int64, error) {
	rd, rc := bufio.UnwrapCountReader(src)
	wr, wc := bufio.UnwrapCountWriter(dst)
	counted := len(rc)+len(wc) > 0
	_, tcpSrc := rd.(*net.TCPConn)
	_, readDeclined := rd.(bufio.ReaderUnwrapper)
	_, writeDeclined := wr.(bufio.WriterUnwrapper)
	if readDeclined || writeDeclined {
		return NewCopySession(dst, src, copySize).run()
	}

	if counted {
		if n, err, ok := splice(wr, rd, rc, wc); ok {
			return n, err
		}
	} else {
		// Nothing to count, so the endpoints beneath pass-through wrappers
		// are equivalent to the originals and expose more.
		dst, src = wr, rd
		if wt, ok := src.(io.WriterTo); ok && (!tcpSrc || kernelCopy(wr) || !canWait) {
			return wt.WriteTo(dst)
		}
	}

	_, extendedRead := rd.(bufio.ExtendedReader)
	_, extendedWrite := wr.(bufio.ExtendedWriter)
	_, wait := rd.(bufio.ReadWaiter)
	if counted || tcpSrc && canWait || extendedRead || extendedWrite || wait {
		return NewCopySession(dst, src, copySize).run()
	}
	if rf, ok := dst.(io.ReaderFrom); ok {
		return rf.ReadFrom(src)
	}
	return CopyBuffered(dst, src)
}

// streamSocket reports whether v is a bare stream socket of the standard
// library, the only kind a descriptor-level path may touch.
func streamSocket(v any) bool {
	switch c := v.(type) {
	case *net.TCPConn:
		return true
	case *net.UnixConn:
		return c.LocalAddr().Network() == "unix"
	}
	return false
}

// CopyBuffered copies through pooled storage using Read and Write only.
func CopyBuffered(dst io.Writer, src io.Reader) (int64, error) {
	b := buf.Get(copySize)
	defer b.Release()
	p := b.FreeBytes()
	var total int64
	empty := 0
	for {
		n, readErr := src.Read(p)
		if n < 0 || n > len(p) {
			return total, bufio.ErrInvalidCount
		}
		if n > 0 {
			empty = 0
			k, err := dst.Write(p[:n:n])
			if k < 0 || k > n {
				return total, bufio.ErrInvalidCount
			}
			total += int64(k)
			if err != nil {
				return total, err
			}
			if k != n {
				return total, io.ErrShortWrite
			}
		} else if readErr == nil {
			empty++
			if empty == maxEmptyReads {
				return total, io.ErrNoProgress
			}
		}
		if readErr == io.EOF {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}

// Counts reports accepted bytes in each direction of CopyConn.
type Counts struct{ AToB, BToA int64 }

// Direction identifies a copy direction relative to the endpoints.
type Direction uint8

const (
	AToB Direction = iota
	BToA
)

// DirectionResult describes copy completion and the subsequent half-close.
// A nil CopyError means the source reached EOF.
type DirectionResult struct {
	Bytes              int64
	CopyError          error
	HalfCloseAttempted bool
	HalfCloseError     error
}

// CopyConn copies both directions and half-closes each destination on EOF
// when supported. A missing CloseWrite method or errors.ErrUnsupported leaves
// the destination open while the reverse direction continues.
// On cancellation or failure it closes both connections and joins both
// directions. Success leaves the connections with the caller. Endpoints
// must implement Close in a way that unblocks pending Read and Write.
//
// The error says what happened to the connections: nil means CopyConn
// closed neither, non-nil means it closed both. When ctx is cancelled as
// both directions finish, either outcome is possible, and each is whole:
// nil with both connections open, or ctx.Err() with both closed. Counts
// report the bytes copied in either case.
func CopyConn(ctx context.Context, a, b net.Conn) (Counts, error) {
	return CopyConnWithObserver(ctx, a, b, nil)
}

// CopyConnWithObserver behaves like CopyConn and reports each completed
// direction. The optional observer may be called concurrently.
func CopyConnWithObserver(ctx context.Context, a, b net.Conn, observe func(Direction, DirectionResult)) (Counts, error) {
	return CopyConnWithOptions(ctx, a, b, CopyConnOptions{Observe: observe})
}

// ErrHalfCloseTimeout reports that the reverse direction did not finish
// within the configured time after a write half-close proved unavailable.
var ErrHalfCloseTimeout = errors.New("transfer: reverse copy timed out after unavailable half-close")

// CopyConnOptions configures optional duplex copy behavior.
type CopyConnOptions struct {
	// HalfCloseTimeout bounds reverse copying after EOF if the destination
	// cannot signal EOF by half-closing. A nonpositive value disables it.
	HalfCloseTimeout time.Duration
	// Observe may be called concurrently and must return promptly.
	Observe func(Direction, DirectionResult)
}

// CopyConnWithOptions behaves like CopyConn and supports a bounded wait when
// half-close is unavailable. Expiry closes both endpoints and joins both copies.
func CopyConnWithOptions(ctx context.Context, a, b net.Conn, options CopyConnOptions) (Counts, error) {
	var once sync.Once
	var cause error
	abort := func(err error) {
		once.Do(func() {
			cause = err
			a.Close()
			b.Close()
		})
	}
	if err := ctx.Err(); err != nil {
		abort(err)
		return Counts{}, err
	}
	doneCancel := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { abort(ctx.Err()); close(doneCancel) })
	var timerOnce sync.Once
	var timer *time.Timer
	var timeoutDone chan struct{}
	copyDirection := func(direction Direction, dst, src net.Conn) int64 {
		n, err := Copy(dst, src)
		result := DirectionResult{Bytes: n, CopyError: err}
		if err == nil {
			if c, ok := dst.(interface{ CloseWrite() error }); ok {
				result.HalfCloseAttempted = true
				result.HalfCloseError = c.CloseWrite()
				if !errors.Is(result.HalfCloseError, errors.ErrUnsupported) {
					err = result.HalfCloseError
				}
			}
			if options.HalfCloseTimeout > 0 &&
				(!result.HalfCloseAttempted || errors.Is(result.HalfCloseError, errors.ErrUnsupported)) {
				timerOnce.Do(func() {
					timeoutDone = make(chan struct{})
					timer = time.AfterFunc(options.HalfCloseTimeout, func() {
						abort(ErrHalfCloseTimeout)
						close(timeoutDone)
					})
				})
			}
		}
		if err != nil {
			abort(err)
		}
		if options.Observe != nil {
			options.Observe(direction, result)
		}
		return n
	}
	reverse := make(chan int64, 1)
	go func() { reverse <- copyDirection(BToA, a, b) }()
	counts := Counts{AToB: copyDirection(AToB, b, a), BToA: <-reverse}
	if timer != nil && !timer.Stop() {
		<-timeoutDone
	}
	if !stop() {
		<-doneCancel
	}
	return counts, cause
}
