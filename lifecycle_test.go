package netx_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ninepeach/go-netx"
)

type blockingService struct {
	started  chan struct{}
	stopped  chan struct{}
	serveErr error
	stopErr  error
	once     sync.Once
}

func (s *blockingService) Serve(ctx context.Context) error {
	close(s.started)
	<-ctx.Done()
	<-s.stopped
	return s.serveErr
}

func (s *blockingService) Shutdown(context.Context) error {
	s.once.Do(func() { close(s.stopped) })
	return s.stopErr
}

func TestServerLifecycleOrderAndExternalShutdown(t *testing.T) {
	service := &blockingService{started: make(chan struct{}), stopped: make(chan struct{})}
	server := netx.NewServer(service)
	server.ShutdownTimeout = time.Second
	var mu sync.Mutex
	var order []string
	add := func(value string) { mu.Lock(); order = append(order, value); mu.Unlock() }
	server.OnStart = func(context.Context) error { add("start"); return nil }
	server.OnStop = func(context.Context) error { add("stop"); return nil }
	done := make(chan error, 1)
	go func() { done <- server.LoopContext(context.Background()) }()
	select {
	case <-service.started:
	case <-time.After(time.Second):
		t.Fatal("service did not start")
	}
	if got := server.State(); got != netx.StateRunning {
		t.Fatalf("state %s", got)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "start" || order[1] != "stop" {
		t.Fatalf("order %v", order)
	}
	if got := server.State(); got != netx.StateStopped {
		t.Fatalf("state %s", got)
	}
}

func TestServerSupportsLegacyFunctionAdapter(t *testing.T) {
	run := make(chan struct{})
	closed := make(chan struct{})
	server := netx.NewServer(netx.ServiceFuncs{
		ServeFunc:    func(context.Context) error { close(run); <-closed; return nil },
		ShutdownFunc: func(context.Context) error { close(closed); return nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.LoopContext(ctx) }()
	<-run
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestServerJoinsServeAndShutdownErrors(t *testing.T) {
	serveErr := errors.New("serve failed")
	shutdownErr := errors.New("shutdown failed")
	server := netx.NewServer(netx.ServiceFuncs{
		ServeFunc:    func(context.Context) error { return serveErr },
		ShutdownFunc: func(context.Context) error { return shutdownErr },
	})
	err := server.LoopContext(context.Background())
	if !errors.Is(err, serveErr) || !errors.Is(err, shutdownErr) {
		t.Fatalf("got %v", err)
	}
}

func TestServerValidatesConfigurationAndIsOneShot(t *testing.T) {
	server := netx.NewServer()
	if err := server.LoopContext(context.Background()); !errors.Is(err, netx.ErrServiceRequired) {
		t.Fatalf("got %v", err)
	}
	if err := server.LoopContext(context.Background()); !errors.Is(err, netx.ErrAlreadyRunning) {
		t.Fatalf("got %v", err)
	}

	conflict := netx.NewServer(netx.ServiceFuncs{ServeFunc: func(context.Context) error { return nil }})
	conflict.OnServe = func(context.Context) error { return nil }
	if err := conflict.LoopContext(context.Background()); !errors.Is(err, netx.ErrServiceConflict) {
		t.Fatalf("got %v", err)
	}
}

func TestServerConvertsHookPanicToError(t *testing.T) {
	server := netx.NewServer()
	server.OnServe = func(context.Context) error { panic("boom") }
	err := server.LoopContext(context.Background())
	if err == nil {
		t.Fatal("expected panic error")
	}
}

func TestServerEnforcesShutdownTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	server := netx.NewServer(netx.ServiceFuncs{
		ServeFunc: func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			<-release
			return nil
		},
		ShutdownFunc: func(context.Context) error { <-release; return nil },
	})
	server.ShutdownTimeout = 20 * time.Millisecond
	done := make(chan error, 1)
	go func() { done <- server.LoopContext(ctx) }()
	<-started
	cancel()
	select {
	case err := <-done:
		close(release)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown timeout was not enforced")
	}
}
