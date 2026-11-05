//go:build unix

package udp

import (
	"context"
	"errors"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
)

// Checks: OBS-04, UDP-09, UDP-11
func TestFatalReadErrorStopsServe(t *testing.T) {
	c, err := Listen(context.Background(), "udp", "127.0.0.1:0", func(*Conn, *Packet) {}, Options{Readers: 2})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error)
	go func() { done <- c.Serve(context.Background()) }()
	time.Sleep(10 * time.Millisecond)

	var backoff time.Duration
	transient := &net.OpError{Op: "read", Err: os.NewSyscallError("recvmsg", syscall.ENOBUFS)}
	if c.readFailed(&c.readers[0], transient, &backoff) {
		t.Fatal("transient error stopped the reader")
	}
	fatal := &net.OpError{Op: "read", Err: os.NewSyscallError("recvmsg", syscall.EBADF)}
	if !c.readFailed(&c.readers[0], fatal, &backoff) {
		t.Fatal("fatal error did not stop the reader")
	}
	select {
	case err := <-done:
		if !errors.Is(err, syscall.EBADF) {
			t.Fatalf("Serve returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("other readers kept running after a fatal error")
	}
	if s := c.Stats(); s.RecvErrors != 2 {
		t.Fatalf("%+v", s)
	}
}

// Checks: UDP-10
func TestCloseInterruptsBackoff(t *testing.T) {
	c, err := Listen(context.Background(), "udp", "127.0.0.1:0", func(*Conn, *Packet) {}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	backoff := 500 * time.Millisecond // next wait would be 1s
	transient := &net.OpError{Op: "read", Err: os.NewSyscallError("recvmsg", syscall.ENOBUFS)}
	stopped := make(chan bool)
	go func() { stopped <- c.readFailed(&c.readers[0], transient, &backoff) }()
	time.Sleep(20 * time.Millisecond)
	start := time.Now()
	c.Close()
	select {
	case stop := <-stopped:
		if !stop {
			t.Fatal("reader not told to stop after Close")
		}
		if d := time.Since(start); d > 200*time.Millisecond {
			t.Fatalf("Close waited %v for the backoff", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("backoff not interrupted")
	}
}
