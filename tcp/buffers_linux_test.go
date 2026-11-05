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

// Checks: DIAL-06
func TestDialerAppliesSocketOptions(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	d := tcp.NewDialer(tcp.DialOptions{Socket: socket.Options{RecvBuffer: 16 << 10}})
	c, err := d.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	rc, _ := c.(*net.TCPConn).SyscallConn()
	var v int
	rc.Control(func(fd uintptr) {
		v, _ = syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF)
	})
	if v != 32<<10 { // Linux doubles the requested size
		t.Fatalf("dialed SO_RCVBUF=%d", v)
	}
}
