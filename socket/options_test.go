//go:build unix

package socket_test

import (
	"context"
	"net"
	"syscall"
	"testing"

	"github.com/ninepeach/go-netx/socket"
)

func socketIntOption(t *testing.T, c syscall.Conn, level, name int) int {
	t.Helper()
	raw, err := c.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var value int
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		value, controlErr = syscall.GetsockoptInt(int(fd), level, name)
	}); err != nil {
		t.Fatal(err)
	}
	if controlErr != nil {
		t.Fatal(controlErr)
	}
	return value
}

// Checks: SOCK-01
func TestListenConfigWithoutOptions(t *testing.T) {
	lc := socket.ListenConfig(socket.Options{})
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if _, ok := ln.Addr().(*net.TCPAddr); !ok {
		t.Fatalf("listener address type = %T", ln.Addr())
	}
}

// Checks: SOCK-02, SOCK-03
func TestListenConfigAppliesBuffersBeforeBind(t *testing.T) {
	const requested = 32 << 10
	lc := socket.ListenConfig(socket.Options{
		RecvBuffer: requested,
		SendBuffer: requested,
	})
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	tcpLn, ok := ln.(*net.TCPListener)
	if !ok {
		t.Fatalf("listener type = %T, want *net.TCPListener", ln)
	}
	if got := socketIntOption(t, tcpLn, syscall.SOL_SOCKET, syscall.SO_RCVBUF); got < requested {
		t.Fatalf("SO_RCVBUF=%d, want at least %d", got, requested)
	}
	if got := socketIntOption(t, tcpLn, syscall.SOL_SOCKET, syscall.SO_SNDBUF); got < requested {
		t.Fatalf("SO_SNDBUF=%d, want at least %d", got, requested)
	}
}

// Checks: SOCK-09
func TestDialerCopiesBaseAndAppliesBuffers(t *testing.T) {
	const requested = 32 << 10
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	base := net.Dialer{}
	configured := socket.Dialer(base, socket.Options{
		RecvBuffer: requested,
		SendBuffer: requested,
	})
	if base.Control != nil {
		t.Fatal("socket.Dialer mutated the supplied Dialer")
	}
	client, err := configured.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	tcpConn, ok := client.(*net.TCPConn)
	if !ok {
		t.Fatalf("connection type = %T, want *net.TCPConn", client)
	}
	if got := socketIntOption(t, tcpConn, syscall.SOL_SOCKET, syscall.SO_RCVBUF); got < requested {
		t.Fatalf("SO_RCVBUF=%d, want at least %d", got, requested)
	}
	if got := socketIntOption(t, tcpConn, syscall.SOL_SOCKET, syscall.SO_SNDBUF); got < requested {
		t.Fatalf("SO_SNDBUF=%d, want at least %d", got, requested)
	}
}

// Checks: SOCK-11
func TestSetBuffersOnExistingConnection(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := <-accepted
	defer server.Close()

	const requested = 24 << 10
	if err := socket.SetBuffers(client.(*net.TCPConn), socket.Options{
		RecvBuffer: requested,
		SendBuffer: requested,
	}); err != nil {
		t.Fatal(err)
	}
	if got := socketIntOption(t, client.(*net.TCPConn), syscall.SOL_SOCKET, syscall.SO_RCVBUF); got < requested {
		t.Fatalf("SO_RCVBUF=%d, want at least %d", got, requested)
	}
	if got := socketIntOption(t, client.(*net.TCPConn), syscall.SOL_SOCKET, syscall.SO_SNDBUF); got < requested {
		t.Fatalf("SO_SNDBUF=%d, want at least %d", got, requested)
	}
}
