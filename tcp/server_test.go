package tcp_test

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
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
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met")
		}
		time.Sleep(time.Millisecond)
	}
}

// Checks: OBS-01, TCP-01, TCP-13
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

// Checks: TCP-12
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

// Checks: OBS-01, TCP-14, TCP-15
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

// Checks: TCP-16, TCP-18
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

// Checks: TCP-17
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

// Checks: TCP-04, TCP-20
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

// brokenListener accepts from inner once, then fails permanently.
type brokenListener struct {
	net.Listener
	n int
}

func (l *brokenListener) Accept() (net.Conn, error) {
	l.n++
	if l.n > 1 {
		return nil, errors.New("listener broken")
	}
	return l.Listener.Accept()
}

// Checks: TCP-06
func TestPermanentAcceptErrorAbortsHandlers(t *testing.T) {
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s, err := tcp.NewServer(&brokenListener{Listener: inner}, func(ctx context.Context, c *tcp.Conn) {
		<-ctx.Done() // long-lived: only abort ends it
	}, tcp.Options{})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error)
	go func() { done <- s.Serve(context.Background()) }()
	dial(t, s)
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "listener broken") {
			t.Fatalf("Serve returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve waited forever on a long-lived handler")
	}
}

// tempErrListener fails every Accept with a temporary error until closed.
type tempErrListener struct {
	net.Listener
	calls atomic.Int32
}

type tempErr struct{}

func (tempErr) Error() string   { return "temporary accept failure" }
func (tempErr) Timeout() bool   { return false }
func (tempErr) Temporary() bool { return true }

func (l *tempErrListener) Accept() (net.Conn, error) {
	l.calls.Add(1)
	return nil, tempErr{}
}

// Checks: TCP-05, TCP-07, OBS-02
func TestTemporaryAcceptErrorsBackOffAndStopPromptly(t *testing.T) {
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln := &tempErrListener{Listener: inner}
	s, err := tcp.NewServer(ln, echo, tcp.Options{})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error)
	go func() { done <- s.Serve(context.Background()) }()
	// Temporary errors are retried, not fatal: several attempts, counted.
	// After 8 errors the next delay is 1s (5ms doubling, capped), so the
	// old uninterruptible sleep would hold Shutdown for most of a second.
	waitFor(t, func() bool { return s.Stats().AcceptErrors >= 8 })
	start := time.Now()
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Serve returned %v for temporary errors", err)
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("Shutdown waited %v for the accept backoff", d)
	}
}

// Checks: TCP-02
func TestListenAddressInUse(t *testing.T) {
	s, _ := start(t, echo, tcp.Options{})
	if _, err := tcp.Listen(context.Background(), "tcp", s.Addr().String(), echo, tcp.Options{}); err == nil {
		t.Fatal("second Listen on a bound port succeeded")
	}
}
