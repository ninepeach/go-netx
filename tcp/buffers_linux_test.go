//go:build linux

package tcp_test

import (
	"context"
	"net"
	"syscall"
	"testing"

	"github.com/ninepeach/go-netx/socket"
	"github.com/ninepeach/go-netx/tcp"
)

// Checks: SOCK-04
func TestAcceptedConnInheritsBuffers(t *testing.T) {
	got := make(chan int, 1)
	s, _ := start(t, func(_ context.Context, c *tcp.Conn) {
		rc, _ := c.Conn.(*net.TCPConn).SyscallConn()
		rc.Control(func(fd uintptr) {
			v, _ := syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF)
			got <- v
		})
	}, tcp.Options{Socket: socket.Options{RecvBuffer: 16 << 10}})
	dial(t, s)
	if v := <-got; v != 32<<10 { // Linux doubles the requested size
		t.Fatalf("SO_RCVBUF=%d", v)
	}
}
