package udp

import (
	"context"
	"errors"
	"net"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"
)

func TestFatalReadErrorStopsServe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("errno values differ")
	}
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
