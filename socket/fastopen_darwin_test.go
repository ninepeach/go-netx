package socket_test

import (
	"context"
	"net"
	"syscall"
	"testing"

	"github.com/ninepeach/go-netx/socket"
)

// Checks: SOCK-26
func TestFastOpenOnDarwin(t *testing.T) {
	opts := socket.Options{FastOpen: socket.FastOpenOptions{Enabled: true}}
	lc := socket.ListenConfig(opts)
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	raw, _ := ln.(*net.TCPListener).SyscallConn()
	var v int
	var gerr error
	raw.Control(func(fd uintptr) { v, gerr = syscall.GetsockoptInt(int(fd), syscall.IPPROTO_TCP, 0x105) })
	if gerr != nil || v == 0 {
		t.Fatalf("TCP_FASTOPEN on the listener = %d, %v; want enabled", v, gerr)
	}
	// Dialing has no Fast Open on macOS (connectx); it runs without.
	d := socket.Dialer(net.Dialer{}, opts)
	c, err := d.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
}
