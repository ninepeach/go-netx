package netx_test

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ninepeach/go-netx"
	"github.com/ninepeach/go-netx/tcp"
	"github.com/ninepeach/go-netx/udp"
)

// fake records lifecycle calls. Serve blocks until aborted or shut down.
type fake struct {
	mu       sync.Mutex
	calls    []string
	stopped  chan struct{}
	once     sync.Once
	serveErr error
}

func newFake() *fake { return &fake{stopped: make(chan struct{})} }

func (f *fake) record(s string) { f.mu.Lock(); f.calls = append(f.calls, s); f.mu.Unlock() }
func (f *fake) log() string     { f.mu.Lock(); defer f.mu.Unlock(); return strings.Join(f.calls, ",") }

func (f *fake) Serve(ctx context.Context) error {
	f.record("serve")
	if f.serveErr != nil {
		return f.serveErr
	}
	select {
	case <-ctx.Done():
		f.record("abort")
	case <-f.stopped:
	}
	return nil
}

func (f *fake) Shutdown(context.Context) error {
	f.record("shutdown")
	f.once.Do(func() { close(f.stopped) })
	return nil
}

func TestOrderAndShutdown(t *testing.T) {
	f := newFake()
	s := netx.NewServer(f)
	s.OnStart = func(context.Context) error { f.record("start"); return nil }
	s.OnStop = func(context.Context) error { f.record("stop"); return nil }
	done := make(chan error)
	go func() { done <- s.LoopContext(context.Background()) }()
	for s.State() != netx.StateRunning {
		time.Sleep(time.Millisecond)
	}
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// Drained gracefully: Serve returned before abort, so no "abort".
	if got := f.log(); got != "start,serve,shutdown,stop" {
		t.Fatal(got)
	}
	if s.State() != netx.StateStopped {
		t.Fatal(s.State())
	}
}

func TestParentCancelDrainsNotAborts(t *testing.T) {
	f := newFake()
	s := netx.NewServer(f)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- s.LoopContext(ctx) }()
	time.Sleep(10 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.log(), "abort") {
		t.Fatalf("aborted instead of draining: %s", f.log())
	}
}

func TestServeErrorStopsAndIsReturned(t *testing.T) {
	f := newFake()
	f.serveErr = errors.New("boom")
	err := netx.NewServer(f).LoopContext(context.Background())
	if !errors.Is(err, f.serveErr) {
		t.Fatal(err)
	}
}

func TestStartErrorShutsDown(t *testing.T) {
	f := newFake()
	s := netx.NewServer(f)
	s.OnStart = func(context.Context) error { return errors.New("no") }
	if err := s.LoopContext(context.Background()); err == nil || f.log() != "shutdown" {
		t.Fatalf("err=%v log=%s", err, f.log())
	}
}

func TestOneShotAndPanic(t *testing.T) {
	s := netx.NewServer(netx.ServiceFuncs{ServeFunc: func(context.Context) error { panic("x") }})
	if err := s.LoopContext(context.Background()); err == nil || !strings.Contains(err.Error(), "panic") {
		t.Fatal(err)
	}
	if err := s.LoopContext(context.Background()); err != netx.ErrAlreadyRunning {
		t.Fatal(err)
	}
}

func TestStuckServiceBoundedByAbortGrace(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	s := netx.NewServer(netx.ServiceFuncs{ServeFunc: func(context.Context) error { <-block; return nil }})
	s.ShutdownTimeout = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := s.LoopContext(ctx); !errors.Is(err, netx.ErrAbortTimeout) {
		t.Fatal(err)
	}
	if time.Since(start) > 3*netx.AbortGrace {
		t.Fatal("not bounded")
	}
}

func TestGroupWithRealTransports(t *testing.T) {
	ctx := context.Background()
	ts, err := tcp.Listen(ctx, "tcp", "127.0.0.1:0", func(_ context.Context, c *tcp.Conn) { _, _ = io.Copy(c, c) }, tcp.Options{})
	if err != nil {
		t.Fatal(err)
	}
	us, err := udp.Listen(ctx, "udp", "127.0.0.1:0", func(c *udp.Conn, p *udp.Packet) { _ = c.Reply(p, p.Data) }, udp.Options{})
	if err != nil {
		t.Fatal(err)
	}
	s := netx.NewServer(netx.Group(ts, us))
	done := make(chan error)
	go func() { done <- s.LoopContext(ctx) }()
	for s.State() != netx.StateRunning {
		time.Sleep(time.Millisecond)
	}

	c, err := net.Dial("tcp", ts.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	c.Write([]byte("hi"))
	in := make([]byte, 2)
	if _, err := io.ReadFull(c, in); err != nil {
		t.Fatal(err)
	}
	c.Close()

	sctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.Shutdown(sctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestOnStopGetsFreshContext(t *testing.T) {
	s := netx.NewServer(slowDrain{})
	s.ShutdownTimeout = 20 * time.Millisecond
	var stopErr error
	s.OnStop = func(ctx context.Context) error { stopErr = ctx.Err(); return nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.LoopContext(ctx)
	if stopErr != nil {
		t.Fatalf("OnStop got an expired context: %v", stopErr)
	}
}

// slowDrain uses up the whole shutdown budget.
type slowDrain struct{}

func (slowDrain) Serve(ctx context.Context) error { <-ctx.Done(); return nil }
func (slowDrain) Shutdown(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}


func TestOnStopErrorIsReturned(t *testing.T) {
	stopErr := errors.New("stop hook failed")
	s := netx.NewServer(netx.ServiceFuncs{
		ServeFunc: func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		},
	})
	s.OnStop = func(context.Context) error { return stopErr }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.LoopContext(ctx) }()
	for s.State() != netx.StateRunning {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, stopErr) {
		t.Fatalf("LoopContext error=%v, want %v", err, stopErr)
	}
}

func TestOnStopHasIndependentTimeoutBudget(t *testing.T) {
	s := netx.NewServer(netx.ServiceFuncs{
		ServeFunc: func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		},
		ShutdownFunc: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})
	s.ShutdownTimeout = 20 * time.Millisecond
	var onStopErr error
	s.OnStop = func(ctx context.Context) error {
		<-ctx.Done()
		onStopErr = ctx.Err()
		return onStopErr
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.LoopContext(ctx) }()
	for s.State() != netx.StateRunning {
		time.Sleep(time.Millisecond)
	}
	cancel()
	err := <-done
	if !errors.Is(onStopErr, context.DeadlineExceeded) {
		t.Fatalf("OnStop context error=%v", onStopErr)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("LoopContext error=%v, want OnStop deadline error", err)
	}
}
