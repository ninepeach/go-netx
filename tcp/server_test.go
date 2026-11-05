package tcp_test

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/ninepeach/go-netx/limit"
	"github.com/ninepeach/go-netx/tcp"
)

func echo(_ context.Context, c *tcp.Conn) { _, _ = io.Copy(c, c) }

func start(t *testing.T, h tcp.Handler, o tcp.Options) (*tcp.Server, chan error) {
	t.Helper()
	s, err := tcp.Listen(context.Background(), "tcp", "127.0.0.1:0", h, o)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Serve(context.Background()) }()
	t.Cleanup(func() { _ = s.Close() })
	return s, done
}

func dial(t *testing.T, s *tcp.Server) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", s.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	c.SetDeadline(time.Now().Add(2 * time.Second))
	return c
}

func roundTrip(t *testing.T, c net.Conn, msg string) {
	t.Helper()
	c.Write([]byte(msg))
	got := make([]byte, len(msg))
	if _, err := io.ReadFull(c, got); err != nil || string(got) != msg {
		t.Fatalf("got %q err %v", got, err)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestEchoAndStats(t *testing.T) {
	s, _ := start(t, echo, tcp.Options{})
	c := dial(t, s)
	roundTrip(t, c, "hello")
	c.Close()
	waitFor(t, func() bool { return s.Stats().Closed == 1 })
	if st := s.Stats(); st.Accepted != 1 || st.Active != 0 {
		t.Fatalf("%+v", st)
	}
}

func TestConnIDsUnique(t *testing.T) {
	ids := make(chan uint64, 2)
	s, _ := start(t, func(_ context.Context, c *tcp.Conn) { ids <- c.ID() }, tcp.Options{})
	dial(t, s)
	dial(t, s)
	a, b := <-ids, <-ids
	if a == 0 || a == b {
		t.Fatalf("ids %d %d", a, b)
	}
}

func TestAdmitRejects(t *testing.T) {
	s, _ := start(t, echo, tcp.Options{Admit: limit.NewConcurrency(1)})
	roundTrip(t, dial(t, s), "a")
	c2 := dial(t, s)
	if _, err := c2.Read(make([]byte, 1)); err == nil {
		t.Fatal("second connection admitted")
	}
	if st := s.Stats(); st.Rejected != 1 {
		t.Fatalf("%+v", st)
	}
}

func TestShutdownDrainsThenReturns(t *testing.T) {
	gotDraining := make(chan struct{})
	s, done := start(t, func(ctx context.Context, c *tcp.Conn) {
		<-c.Draining()
		if ctx.Err() != nil {
			t.Error("handler context cancelled during drain")
		}
		close(gotDraining)
	}, tcp.Options{})
	dial(t, s)
	waitFor(t, func() bool { return s.Stats().Active == 1 })
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-gotDraining
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := net.Dial("tcp", s.Addr().String()); err == nil {
		t.Fatal("still accepting")
	}
}

func TestShutdownAbortsOnTimeout(t *testing.T) {
	aborted := make(chan struct{})
	s, done := start(t, func(ctx context.Context, c *tcp.Conn) {
		<-ctx.Done() // ignores Draining; only Abort ends it
		close(aborted)
	}, tcp.Options{})
	c := dial(t, s)
	waitFor(t, func() bool { return s.Stats().Active == 1 })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := s.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	<-aborted
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := c.Read(make([]byte, 1)); err == nil {
		t.Fatal("connection not closed on abort")
	}
}

func TestServeContextCancelAborts(t *testing.T) {
	s, err := tcp.Listen(context.Background(), "tcp", "127.0.0.1:0", func(ctx context.Context, _ *tcp.Conn) { <-ctx.Done() }, tcp.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- s.Serve(ctx) }()
	dial(t, s)
	waitFor(t, func() bool { return s.Stats().Active == 1 })
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after cancel")
	}
	if err := s.Serve(context.Background()); err != tcp.ErrClosed {
		t.Fatal(err)
	}
}
