//go:build darwin

package socket_test

import (
	"context"
	"net"
	"syscall"
	"testing"

	"github.com/ninepeach/go-netx/socket"
)

// Checks: SOCK-18
func TestReusePortOnDarwin(t *testing.T) {
	ln, err := socket.ListenConfig(socket.Options{ReusePort: true}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	tcpLn, ok := ln.(*net.TCPListener)
	if !ok {
		t.Fatalf("listener type = %T, want *net.TCPListener", ln)
	}
	if got := socketIntOption(t, tcpLn, syscall.SOL_SOCKET, syscall.SO_REUSEPORT); got != 1 {
		t.Fatalf("SO_REUSEPORT=%d, want 1", got)
	}
}
