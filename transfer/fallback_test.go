package transfer

import (
	"bytes"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/ninepeach/go-netx/buf"
	"github.com/ninepeach/go-netx/bufio"
)

type readOnly struct{ io.Reader }
type writeOnly struct{ io.Writer }
type idleReader struct{}

func (idleReader) Read([]byte) (int, error) { return 0, nil }

func TestCopyBufferedFallback(t *testing.T) {
	if n, err := CopyBuffered(writeOnly{io.Discard}, idleReader{}); n != 0 || err != io.ErrNoProgress {
		t.Fatalf("empty copy = %d, %v", n, err)
	}
	if n, err := CopyBuffered(partialSessionWriter{}, readOnly{strings.NewReader("abcd")}); n != 2 || err != io.ErrClosedPipe {
		t.Fatalf("partial copy = %d, %v", n, err)
	}
}

func TestCachedPrefixAndRefresh(t *testing.T) {
	var dst sessionSink
	r := bufio.NewCachedReader(strings.NewReader("tail"), buf.Copy([]byte("head:")))
	defer r.Close()
	s := NewCopySession(&dst, r, 5)
	if n, err := s.Step(); n != 5 || err != nil {
		t.Fatalf("cached step = %d, %v", n, err)
	}
	s.SetEndpoints(&dst, strings.NewReader("tail"))
	if n, err := s.Step(); n != 4 || err != nil || dst.String() != "head:tail" || s.Transferred() != 9 {
		t.Fatalf("copy = %d, %v, %q", n, err, dst.String())
	}
}

func TestExtendedCopy(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("stream"), bufio.ReaderOptions{})
	defer r.Release()
	var dst sessionSink
	if n, err := Copy(&dst, r); n != 6 || err != nil || dst.String() != "stream" || dst.extended == 0 {
		t.Fatalf("extended = %d, %v, %q", n, err, dst.String())
	}
}

func TestCachedConnCopy(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	c := bufio.NewCachedConn(a, buf.Copy([]byte("prefix:")))
	defer c.Close()
	go func() { b.Write([]byte("body")); b.Close() }()
	var dst bytes.Buffer
	if n, err := Copy(&dst, c); n != 11 || err != nil || dst.String() != "prefix:body" {
		t.Fatalf("copy = %d, %v, %q", n, err, dst.String())
	}
}

func TestCopyConnCancellation(t *testing.T) {
	a, peerA := net.Pipe()
	b, peerB := net.Pipe()
	defer peerA.Close()
	defer peerB.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	var counts Counts
	var err error
	go func() { counts, err = CopyConn(ctx, a, b); close(done) }()
	if err := peerB.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := peerA.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	go peerA.Write([]byte("x"))
	var p [1]byte
	if _, err := io.ReadFull(peerB, p[:]); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-done:
		if err != context.Canceled || counts.AToB != 1 || counts.BToA != 0 {
			t.Fatalf("copy = %+v, %v", counts, err)
		}
	case <-time.After(time.Second):
		t.Fatal("copy did not join both directions")
	}
}
