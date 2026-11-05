//go:build darwin

package tcp_test

import (
	"context"
	"net"
	"syscall"
	"testing"

	"github.com/ninepeach/go-netx/socket"
	"github.com/ninepeach/go-netx/tcp"
)

func TestAcceptedConnInheritsBuffersDarwin(t *testing.T) {
	const requested = 16 << 10
	got := make(chan int, 1)
	s, _ := start(t, func(_ context.Context, c *tcp.Conn) {
		rc, err := c.Conn.(*net.TCPConn).SyscallConn()
		if err != nil {
			t.Error(err)
			return
		}
		var value int
		var controlErr error
		if err := rc.Control(func(fd uintptr) {
			value, controlErr = syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF)
		}); err != nil {
			t.Error(err)
			return
		}
		if controlErr != nil {
			t.Error(controlErr)
			return
		}
		got <- value
	}, tcp.Options{Socket: socket.Options{RecvBuffer: requested}})
	dial(t, s)
	if value := <-got; value < requested {
		t.Fatalf("accepted SO_RCVBUF=%d, want at least %d", value, requested)
	}
}
