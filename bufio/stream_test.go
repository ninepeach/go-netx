package bufio

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ninepeach/go-netx/buf"
)

type vectorSink struct {
	bytes.Buffer
	called bool
}

func (w *vectorSink) WriteVectorised(p [][]byte) (int64, error) {
	w.called = true
	n, _ := w.Buffer.Write(p[0])
	return int64(n), io.ErrClosedPipe
}

func TestVectorPartialDoesNotRetry(t *testing.T) {
	dst := new(vectorSink)
	w := NewWriter(dst)
	w.Add(buf.Copy([]byte("first")))
	w.Add(buf.Copy([]byte("next")))
	n, err := w.Flush()
	if !dst.called || n != 5 || !errors.Is(err, io.ErrClosedPipe) || dst.String() != "first" {
		t.Fatalf("Flush = %d, %v, %q", n, err, dst.String())
	}
	if count, size := w.Pending(); count != 0 || size != 0 {
		t.Fatal("retained a failed batch")
	}
}

func TestReadBufferUsesBufferedPrefix(t *testing.T) {
	r := NewReader(strings.NewReader("abc"), ReaderOptions{})
	defer r.Release()
	if _, err := r.Peek(2); err != nil {
		t.Fatal(err)
	}
	b, err := ReadBuffer(r, 2)
	if b == nil || err != nil {
		t.Fatalf("read buffer: %v", err)
	}
	defer b.Release()
	if string(b.Bytes()) != "ab" {
		t.Fatalf("prefix: %q", b.Bytes())
	}
}

func TestCachedConcurrentClose(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	c := NewCachedConn(a, buf.Copy([]byte("unused")))
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.Close(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if c.prefix != nil {
		t.Fatal("Close retained the prefix")
	}
}

func TestTCPReadWaiter(t *testing.T) {
	src, peer := tcpPair(t)
	defer src.Close()
	defer peer.Close()
	w, ok := NewReadWaiter(src)
	if !ok {
		if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
			t.Fatal("bare TCP waiter missing")
		}
		return
	}
	if err := src.SetReadDeadline(time.Now()); err != nil {
		t.Fatal(err)
	}
	if b, err := w.WaitReadBuffer(16); b != nil || !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("deadline = %v, %v", b, err)
	}
	if err := src.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := peer.Write([]byte("data")); err != nil {
		t.Fatal(err)
	}
	b, err := w.WaitReadBuffer(16)
	if b == nil || err != nil {
		t.Fatalf("read = %v, %v", b, err)
	}
	if string(b.Bytes()) != "data" {
		t.Errorf("got %q", b.Bytes())
	}
	b.Release()
	if err := peer.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if b, err := w.WaitReadBuffer(16); b != nil || err != io.EOF {
		t.Fatalf("EOF = %v, %v", b, err)
	}
}

func tcpPair(t testing.TB) (*net.TCPConn, *net.TCPConn) {
	t.Helper()
	l, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	a, err := net.DialTCP("tcp4", nil, l.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	b, err := l.AcceptTCP()
	if err != nil {
		a.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close(); b.Close() })
	return a, b
}

func TestCachedReaderAndComposeConn(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	r := NewCachedReader(strings.NewReader("body"), buf.Copy([]byte("head:")))
	defer r.Close()
	c := WithAddresses(ComposeConn(a, r, io.Discard), b.LocalAddr(), nil)
	p, err := io.ReadAll(c)
	if err != nil || string(p) != "head:body" {
		t.Fatalf("read = %q, %v", p, err)
	}
	if n, err := c.Write([]byte("out")); n != 3 || err != nil {
		t.Fatalf("write = %d, %v", n, err)
	}
	if c.RemoteAddr() != a.RemoteAddr() {
		t.Fatal("remote address changed")
	}
	if err := c.(interface{ CloseWrite() error }).CloseWrite(); err != errors.ErrUnsupported {
		t.Fatalf("half-close = %v", err)
	}
}

func TestCheckedWriteBuffer(t *testing.T) {
	b := buf.Copy([]byte("x"))
	defer b.Release()
	var c bytes.Buffer
	if n, err := WriteBuffer(&c, b); n != 1 || err != nil || b.Len() != 1 {
		t.Fatalf("write = %d, %v, len %d", n, err, b.Len())
	}
}

var _ ExtendedConn = (*CachedConn)(nil)
