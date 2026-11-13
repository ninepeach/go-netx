package tcp_test

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ninepeach/go-netx/bufio"
	"github.com/ninepeach/go-netx/tcp"
)

func start(t *testing.T, handler tcp.Handler, options tcp.Options) (*tcp.Server, <-chan error) {
	t.Helper()
	server, err := tcp.Listen(context.Background(), "tcp", "127.0.0.1:0", handler, options)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background()) }()
	t.Cleanup(func() { _ = server.Close() })
	return server, done
}

func dial(t *testing.T, server *tcp.Server) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", server.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	return conn
}

func wait(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestEchoStatsAndUniqueIDs(t *testing.T) {
	ids := make(chan uint64, 2)
	server, _ := start(t, func(_ context.Context, conn *tcp.Conn) {
		ids <- conn.ID()
		_, _ = io.Copy(conn, conn)
	}, tcp.Options{})
	for _, message := range []string{"one", "two"} {
		conn := dial(t, server)
		if _, err := io.WriteString(conn, message); err != nil {
			t.Fatal(err)
		}
		got := make([]byte, len(message))
		if _, err := io.ReadFull(conn, got); err != nil || string(got) != message {
			t.Fatalf("got %q, err %v", got, err)
		}
		_ = conn.Close()
	}
	a, b := <-ids, <-ids
	if a == 0 || a == b {
		t.Fatalf("IDs %d and %d", a, b)
	}
	wait(t, func() bool { return server.Stats().Closed == 2 })
	if stats := server.Stats(); stats.Accepted != 2 || stats.Active != 0 {
		t.Fatalf("stats: %+v", stats)
	}
}

func TestAdmissionLimits(t *testing.T) {
	block := make(chan struct{})
	server, _ := start(t, func(_ context.Context, _ *tcp.Conn) { <-block },
		tcp.Options{MaxConnections: 1, MaxPerIP: 1})
	first := dial(t, server)
	wait(t, func() bool { return server.Stats().Active == 1 })
	second := dial(t, server)
	if _, err := second.Read(make([]byte, 1)); err == nil {
		t.Fatal("second connection admitted")
	}
	if stats := server.Stats(); stats.Rejected+stats.RejectedPerIP != 1 {
		t.Fatalf("stats: %+v", stats)
	}
	close(block)
	_ = first.Close()
}

func TestShutdownDrains(t *testing.T) {
	drained := make(chan struct{})
	server, done := start(t, func(ctx context.Context, conn *tcp.Conn) {
		<-conn.Draining()
		if ctx.Err() != nil {
			t.Error("handler context cancelled during drain")
		}
		close(drained)
	}, tcp.Options{})
	dial(t, server)
	wait(t, func() bool { return server.Stats().Active == 1 })
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-drained
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestShutdownDeadlineAborts(t *testing.T) {
	aborted := make(chan struct{})
	server, done := start(t, func(ctx context.Context, _ *tcp.Conn) {
		<-ctx.Done()
		close(aborted)
	}, tcp.Options{})
	conn := dial(t, server)
	wait(t, func() bool { return server.Stats().Active == 1 })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := server.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown: %v", err)
	}
	<-aborted
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("connection remained open")
	}
}

func TestShutdownBeforeServe(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server, err := tcp.NewServer(listener, func(context.Context, *tcp.Conn) {}, tcp.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := server.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := server.Serve(context.Background()); !errors.Is(err, tcp.ErrClosed) {
		t.Fatalf("second Serve: %v", err)
	}
}

// Conn declares itself pass-through, so bufio and transfer reach the socket.
func TestConnIsPassThrough(t *testing.T) {
	var c net.Conn = &net.TCPConn{}
	wrapped := &tcp.Conn{Conn: c}
	if r, counters := bufio.UnwrapCountReader(wrapped); r != io.Reader(c) || counters != nil {
		t.Fatalf("reader unwrapped to %T", r)
	}
	if w, counters := bufio.UnwrapCountWriter(wrapped); w != io.Writer(c) || counters != nil {
		t.Fatalf("writer unwrapped to %T", w)
	}
}

type countedCloseConn struct {
	net.Conn
	closes atomic.Int32
}

func (c *countedCloseConn) Close() error {
	c.closes.Add(1)
	return c.Conn.Close()
}

func TestConnCloseOnceConcurrent(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	raw := &countedCloseConn{Conn: a}
	conn := &tcp.Conn{Conn: raw}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = conn.Close()
		}()
	}
	wg.Wait()
	if got := raw.closes.Load(); got != 1 {
		t.Fatalf("underlying Close called %d times, want 1", got)
	}
}
