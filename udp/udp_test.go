package udp_test

import (
	"context"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ninepeach/go-netx/socket"
	"github.com/ninepeach/go-netx/udp"
)

func echo(c *udp.Conn, p *udp.Packet) { _ = c.Reply(p, p.Data) }

func start(t testing.TB, h udp.Handler, o udp.Options) *udp.Conn {
	t.Helper()
	c, err := udp.Listen(context.Background(), "udp", "127.0.0.1:0", h, o)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- c.Serve(context.Background()) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := c.Shutdown(ctx); err != nil {
			t.Error(err)
		}
		if err := <-done; err != nil && err != udp.ErrClosed {
			t.Error(err)
		}
	})
	return c
}

func dial(t testing.TB, c *udp.Conn) *net.UDPConn {
	t.Helper()
	cl, err := net.DialUDP("udp", nil, net.UDPAddrFromAddrPort(c.LocalAddr()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cl.Close() })
	cl.SetDeadline(time.Now().Add(2 * time.Second))
	return cl
}

func TestEcho(t *testing.T) {
	c := start(t, echo, udp.Options{})
	cl := dial(t, c)
	cl.Write([]byte("hello"))
	in := make([]byte, 64)
	n, err := cl.Read(in)
	if err != nil || string(in[:n]) != "hello" {
		t.Fatalf("%q %v", in[:n], err)
	}
	s := c.Stats()
	if s.RecvPackets != 1 || s.SendPackets != 1 || s.RecvBytes != 5 {
		t.Fatalf("%+v", s)
	}
}

func TestReusePortReaders(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("SO_REUSEPORT is Linux-only in socket")
	}
	var mu sync.Mutex
	seen := map[int]bool{}
	c := start(t, func(c *udp.Conn, p *udp.Packet) {
		mu.Lock()
		seen[p.Reader] = true
		mu.Unlock()
		echo(c, p)
	}, udp.Options{Readers: 4, Socket: socket.Options{ReusePort: true}})
	in := make([]byte, 64)
	for i := 0; i < 32; i++ { // many source ports so the kernel spreads them
		cl := dial(t, c)
		cl.Write([]byte("x"))
		if _, err := cl.Read(in); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) < 2 {
		t.Fatalf("all datagrams on one reader: %v", seen)
	}
}

func TestTruncatedDropped(t *testing.T) {
	got := make(chan int, 2)
	c := start(t, func(_ *udp.Conn, p *udp.Packet) { got <- len(p.Data) }, udp.Options{MaxPacket: 8})
	cl := dial(t, c)
	cl.Write(make([]byte, 100))
	cl.Write(make([]byte, 4))
	if n := <-got; n != 4 {
		t.Fatalf("handler saw %d bytes", n)
	}
	if s := c.Stats(); s.Truncated != 1 {
		t.Fatalf("%+v", s)
	}
}

func TestWriteTooLarge(t *testing.T) {
	c := start(t, echo, udp.Options{MaxPacket: 8})
	if err := c.WriteTo(make([]byte, 9), c.LocalAddr()); err != udp.ErrTooLarge {
		t.Fatal(err)
	}
}

func TestServeAfterCloseAndContextCancel(t *testing.T) {
	c, err := udp.Listen(context.Background(), "udp", "127.0.0.1:0", echo, udp.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- c.Serve(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop on cancel")
	}
	if err := c.Serve(context.Background()); err != udp.ErrClosed {
		t.Fatal(err)
	}
}

func TestHandlerDoesNotAllocate(t *testing.T) {
	c := start(t, echo, udp.Options{})
	cl := dial(t, c)
	msg, in := make([]byte, 512), make([]byte, 2048)
	for i := 0; i < 100; i++ { // warm up
		cl.Write(msg)
		cl.Read(in)
	}
	allocs := testing.AllocsPerRun(1000, func() {
		cl.Write(msg)
		cl.Read(in)
	})
	if allocs > 0 {
		t.Fatalf("%.1f allocs per round trip", allocs)
	}
}

func BenchmarkEcho(b *testing.B) {
	c := start(b, echo, udp.Options{})
	cl := dial(b, c)
	cl.SetDeadline(time.Time{})
	msg, in := make([]byte, 512), make([]byte, 2048)
	b.ReportAllocs()
	b.SetBytes(512)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cl.Write(msg)
		cl.Read(in)
	}
}
