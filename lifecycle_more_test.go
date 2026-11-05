package netx_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ninepeach/go-netx"
)

func waitState(t *testing.T, s *netx.Server, want netx.State) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for s.State() != want {
		if time.Now().After(deadline) {
			t.Fatalf("state %v, want %v", s.State(), want)
		}
		time.Sleep(time.Millisecond)
	}
}

// Checks: RUN-10
func TestStopOutsideRunIsErrNotRunning(t *testing.T) {
	s := netx.NewServer(newFake())
	if err := s.Stop(); err != netx.ErrNotRunning {
		t.Fatalf("Stop before run: %v", err)
	}
	if err := s.Shutdown(context.Background()); err != netx.ErrNotRunning {
		t.Fatalf("Shutdown before run: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.LoopContext(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(); err != netx.ErrNotRunning {
		t.Fatalf("Stop after stop: %v", err)
	}
	if err := s.Shutdown(context.Background()); err != netx.ErrNotRunning {
		t.Fatalf("Shutdown after stop: %v", err)
	}
}

// Checks: RUN-11, RUN-15
func TestConcurrentStopAndShutdown(t *testing.T) {
	s := netx.NewServer(newFake())
	done := make(chan error)
	go func() { done <- s.LoopContext(context.Background()) }()
	waitState(t, s, netx.StateRunning)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				_ = s.Stop() // may race with the loop finishing
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := s.Shutdown(ctx); err != nil && err != netx.ErrNotRunning {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if s.State() != netx.StateStopped {
		t.Fatal(s.State())
	}
}

// failingService returns err from Serve after a short delay.
type failingService struct{ err error }

func (f failingService) Serve(context.Context) error {
	time.Sleep(10 * time.Millisecond)
	return f.err
}
func (failingService) Shutdown(context.Context) error { return nil }

// Checks: RUN-13
func TestGroupAbortsOthersOnError(t *testing.T) {
	boom := errors.New("boom")
	survivor := newFake()
	g := netx.Group(failingService{boom}, survivor)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := g.Serve(ctx); !errors.Is(err, boom) {
		t.Fatalf("Group.Serve returned %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("Group waited for the timeout instead of aborting")
	}
	if survivor.log() != "serve,abort" {
		t.Fatalf("other service not aborted: %s", survivor.log())
	}
}

type shutdownErr struct {
	newFakeSvc *fake
	err        error
}

func (s shutdownErr) Serve(ctx context.Context) error { return s.newFakeSvc.Serve(ctx) }
func (s shutdownErr) Shutdown(ctx context.Context) error {
	s.newFakeSvc.Shutdown(ctx)
	return s.err
}

// Checks: RUN-14
func TestGroupShutdownJoinsErrors(t *testing.T) {
	e1, e2 := errors.New("first"), errors.New("second")
	a, b := shutdownErr{newFake(), e1}, shutdownErr{newFake(), e2}
	g := netx.Group(a, b)
	served := make(chan error)
	go func() { served <- g.Serve(context.Background()) }()
	time.Sleep(5 * time.Millisecond)
	err := g.Shutdown(context.Background())
	if !errors.Is(err, e1) || !errors.Is(err, e2) {
		t.Fatalf("Shutdown error %v does not contain both", err)
	}
	if err := <-served; err != nil {
		t.Fatal(err)
	}
	// Both drained (Serve returned via Shutdown, not abort).
	if a.newFakeSvc.log() != "serve,shutdown" || b.newFakeSvc.log() != "serve,shutdown" {
		t.Fatalf("a=%s b=%s", a.newFakeSvc.log(), b.newFakeSvc.log())
	}
}
