//go:build unix

package netx_test

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/ninepeach/go-netx"
)

// drainForever never finishes draining; only an abort ends it.
type drainForever struct{}

func (drainForever) Serve(ctx context.Context) error { <-ctx.Done(); return nil }
func (drainForever) Shutdown(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

// Checks: RUN-09
func TestSecondSignalAborts(t *testing.T) {
	s := netx.NewServer(drainForever{})
	s.Signals = []os.Signal{syscall.SIGUSR1}
	s.ShutdownTimeout = 0 // unbounded drain: only the second signal can end it
	done := make(chan error)
	go func() { done <- s.Loop() }()
	for s.State() != netx.StateRunning {
		time.Sleep(time.Millisecond)
	}
	syscall.Kill(os.Getpid(), syscall.SIGUSR1)
	for s.State() != netx.StateStopping {
		time.Sleep(time.Millisecond)
	}
	syscall.Kill(os.Getpid(), syscall.SIGUSR1)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("second signal did not abort")
	}
}
