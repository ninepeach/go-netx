package netx_test

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/ninepeach/go-netx"
	"github.com/ninepeach/go-netx/internal/leak"
	"github.com/ninepeach/go-netx/tcp"
	"github.com/ninepeach/go-netx/udp"
)

// After the runtime stops a TCP and a UDP service, every goroutine and
// socket they started is gone.
//
// Checks: RUN-16
func TestStopReleasesGoroutinesAndSockets(t *testing.T) {
	base := leak.Take()
	ctx := context.Background()
	ts, err := tcp.Listen(ctx, "tcp", "127.0.0.1:0", func(ctx context.Context, c *tcp.Conn) {
		_, _ = io.Copy(c, c)
	}, tcp.Options{})
	if err != nil {
		t.Fatal(err)
	}
	us, err := udp.Listen(ctx, "udp", "127.0.0.1:0", func(c *udp.Conn, p *udp.Packet) {
		_ = c.Reply(p, p.Data)
	}, udp.Options{Readers: 4})
	if err != nil {
		t.Fatal(err)
	}
	s := netx.NewServer(netx.Group(ts, us))
	s.ShutdownTimeout = 200 * time.Millisecond
	done := make(chan error)
	go func() { done <- s.LoopContext(ctx) }()
	waitState(t, s, netx.StateRunning)

	// Leave some TCP connections open so shutdown has to abort them.
	var clients []net.Conn
	for i := 0; i < 10; i++ {
		c, err := net.Dial("tcp", ts.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		clients = append(clients, c)
	}
	u, err := net.Dial("udp", us.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	u.Write([]byte("x"))

	// The open connections never finish, so the drain times out and they
	// are aborted: DeadlineExceeded is the expected outcome.
	if err := s.Shutdown(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	<-done
	for _, c := range clients {
		c.Close()
	}
	u.Close()
	if err := base.Check(3*time.Second, 0); err != nil {
		t.Fatal(err)
	}
}
