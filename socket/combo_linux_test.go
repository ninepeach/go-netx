//go:build linux

package socket_test

import (
	"context"
	"net"
	"syscall"
	"testing"

	"github.com/ninepeach/go-netx/socket"
)

const (
	soReusePort  = 15 // SO_REUSEPORT
	requestedBuf = 48 << 10
)

// Checks: SOCK-05
func TestBuffersCombineWithReusePort(t *testing.T) {
	lc := socket.ListenConfig(socket.Options{
		ReusePort:  true,
		RecvBuffer: requestedBuf,
		SendBuffer: requestedBuf,
	})
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	tl := ln.(*net.TCPListener)
	if v := socketIntOption(t, tl, syscall.SOL_SOCKET, soReusePort); v != 1 {
		t.Fatalf("SO_REUSEPORT=%d", v)
	}
	if v := socketIntOption(t, tl, syscall.SOL_SOCKET, syscall.SO_RCVBUF); v < requestedBuf {
		t.Fatalf("SO_RCVBUF=%d", v)
	}
	if v := socketIntOption(t, tl, syscall.SOL_SOCKET, syscall.SO_SNDBUF); v < requestedBuf {
		t.Fatalf("SO_SNDBUF=%d", v)
	}
	// ReusePort really allows a second bind next to the first.
	ln2, err := lc.Listen(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal("second ReusePort bind failed:", err)
	}
	ln2.Close()
}

// Checks: SOCK-10
func TestZeroAndNegativeBuffersAreNotApplied(t *testing.T) {
	plain, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	defRcv := socketIntOption(t, plain.(*net.TCPListener), syscall.SOL_SOCKET, syscall.SO_RCVBUF)
	defSnd := socketIntOption(t, plain.(*net.TCPListener), syscall.SOL_SOCKET, syscall.SO_SNDBUF)
	for _, n := range []int{0, -1, -1 << 20} {
		lc := socket.ListenConfig(socket.Options{RecvBuffer: n, SendBuffer: n})
		ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("size %d: %v", n, err)
		}
		tl := ln.(*net.TCPListener)
		rcv := socketIntOption(t, tl, syscall.SOL_SOCKET, syscall.SO_RCVBUF)
		snd := socketIntOption(t, tl, syscall.SOL_SOCKET, syscall.SO_SNDBUF)
		ln.Close()
		if rcv != defRcv || snd != defSnd {
			t.Fatalf("size %d changed buffers: rcv %d→%d snd %d→%d", n, defRcv, rcv, defSnd, snd)
		}
	}
}
