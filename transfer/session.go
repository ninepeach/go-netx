package transfer

import (
	"io"
	"net"

	"github.com/ninepeach/go-netx/bufio"
)

// CopySession moves one owned chunk per step. Calls must be serialized.
// It borrows both endpoints and holds no buffer between steps.
type CopySession struct {
	dst     io.Writer
	src     io.Reader
	wr      io.Writer // dst beneath its counters
	rd      io.Reader // src beneath its counters, read-waiting if it can
	wc, rc  []bufio.Counter
	size    int
	total   int64
	err     error
	readEOF bool
	empty   int
}

// NewCopySession requires a positive maximum chunk size.
func NewCopySession(dst io.Writer, src io.Reader, size int) *CopySession {
	if size <= 0 {
		panic("transfer: invalid chunk size")
	}
	s := &CopySession{size: size}
	s.SetEndpoints(dst, src)
	return s
}

// SetEndpoints replaces the borrowed endpoints between steps, for example
// after a handshake. It preserves counters and does not clear terminal errors.
//
// Capabilities are looked up again for the new endpoints. Counting wrappers
// are stepped around and credited directly, so they hide nothing beneath
// them; a bare TCP source is read without holding a buffer while idle.
func (s *CopySession) SetEndpoints(dst io.Writer, src io.Reader) {
	s.dst, s.src = dst, src
	s.empty = 0
	s.rd, s.rc = bufio.UnwrapCountReader(src)
	s.wr, s.wc = bufio.UnwrapCountWriter(dst)
	if c, ok := s.rd.(*net.TCPConn); ok {
		if w, ok := bufio.NewReadWaiter(c); ok {
			s.rd = w
		}
	}
}

// Transferred reports the total bytes acknowledged by the writer.
func (s *CopySession) Transferred() int64 { return s.total }

// Step forwards one chunk. Data returned with a read error is written
// before reporting that error. Errors including EOF terminate the session.
func (s *CopySession) Step() (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	b, readErr := bufio.ReadBuffer(s.rd, s.size)
	if b == nil {
		s.err, s.readEOF = readErr, readErr == io.EOF
		if readErr == nil {
			s.empty++
			if s.empty == maxEmptyReads {
				s.err = io.ErrNoProgress
			}
		}
		return 0, s.err
	}
	s.empty = 0
	defer b.Release()
	count(s.rc, int64(b.Len()))
	n, err := bufio.WriteBuffer(s.wr, b)
	count(s.wc, int64(n))
	s.total += int64(n)
	if err != nil {
		s.err = err
	} else {
		s.err, s.readEOF = readErr, readErr == io.EOF
	}
	return n, s.err
}

// run steps until the source ends or an error stops the session.
func (s *CopySession) run() (int64, error) {
	for {
		if _, err := s.Step(); err != nil {
			if err == io.EOF && s.readEOF {
				return s.total, nil
			}
			return s.total, err
		}
	}
}

func count(counters []bufio.Counter, n int64) {
	if n > 0 {
		for _, c := range counters {
			c.Add(n)
		}
	}
}
