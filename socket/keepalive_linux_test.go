//go:build linux

package socket_test

import (
	"context"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/ninepeach/go-netx/socket"
)

type keepAliveState struct{ on, idle, interval, count int }

func keepAliveOf(t *testing.T, c syscall.Conn) keepAliveState {
	t.Helper()
	return keepAliveState{
		on:       socketIntOption(t, c, syscall.SOL_SOCKET, syscall.SO_KEEPALIVE),
		idle:     socketIntOption(t, c, syscall.IPPROTO_TCP, syscall.TCP_KEEPIDLE),
		interval: socketIntOption(t, c, syscall.IPPROTO_TCP, syscall.TCP_KEEPINTVL),
		count:    socketIntOption(t, c, syscall.IPPROTO_TCP, syscall.TCP_KEEPCNT),
	}
}

// accepted listens with opts, connects with a plain dialer and returns the
// server side of the connection.
func accepted(t *testing.T, opts socket.Options) *net.TCPConn {
	t.Helper()
	lc := socket.ListenConfig(opts)
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		c, err := net.Dial("tcp", ln.Addr().String())
		if err == nil {
			t.Cleanup(func() { c.Close() })
		}
	}()
	c, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c.(*net.TCPConn)
}

// Checks: SOCK-13
func TestKeepAliveAppliesToAcceptedConns(t *testing.T) {
	c := accepted(t, socket.Options{KeepAlive: socket.KeepAlive{Idle: 30 * time.Second, Interval: 5 * time.Second, Count: 3}})
	if got := keepAliveOf(t, c); got != (keepAliveState{on: 1, idle: 30, interval: 5, count: 3}) {
		t.Fatalf("accepted conn keepalive %+v", got)
	}
}

// Checks: SOCK-14
func TestKeepAliveDisable(t *testing.T) {
	c := accepted(t, socket.Options{KeepAlive: socket.KeepAlive{Disable: true, Idle: time.Second}})
	if on := keepAliveOf(t, c).on; on != 0 {
		t.Fatalf("SO_KEEPALIVE=%d with Disable", on)
	}
}

// Checks: SOCK-15
func TestKeepAliveAppliesToDialer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		if c, err := ln.Accept(); err == nil {
			defer c.Close()
			c.Read(make([]byte, 1))
		}
	}()
	d := socket.Dialer(net.Dialer{}, socket.Options{KeepAlive: socket.KeepAlive{Idle: 40 * time.Second, Interval: 7 * time.Second, Count: 4}})
	c, err := d.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if got := keepAliveOf(t, c.(*net.TCPConn)); got != (keepAliveState{on: 1, idle: 40, interval: 7, count: 4}) {
		t.Fatalf("dialed conn keepalive %+v", got)
	}

	off := socket.Dialer(net.Dialer{}, socket.Options{KeepAlive: socket.KeepAlive{Disable: true}})
	c2, err := off.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	if on := keepAliveOf(t, c2.(*net.TCPConn)).on; on != 0 {
		t.Fatalf("dialed SO_KEEPALIVE=%d with Disable", on)
	}
}

// Checks: SOCK-16
func TestKeepAliveZeroValueKeepsGoDefault(t *testing.T) {
	c := accepted(t, socket.Options{})
	got := keepAliveOf(t, c)
	if got.on != 1 || got.idle != 15 || got.interval != 15 {
		t.Fatalf("default keepalive %+v, want Go's 15s", got)
	}
}
