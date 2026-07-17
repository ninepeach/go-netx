package tcp_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/ninepeach/go-netx/tcp"
)

func TestDialer(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() { c, _ := ln.Accept(); accepted <- c }()
	d := tcp.NewDialer(tcp.DialOptions{Timeout: time.Second})
	conn, err := d.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	serverConn := <-accepted
	if serverConn == nil {
		t.Fatal("server did not accept")
	}
	serverConn.Close()
}

func TestDialEventSuccess(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, _ := ln.Accept()
		if conn != nil {
			_ = conn.Close()
		}
	}()
	events := make(chan tcp.DialEvent, 1)
	dialer := tcp.NewDialer(tcp.DialOptions{OnDial: func(event tcp.DialEvent) { events <- event }})
	conn, err := dialer.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	event := <-events
	if event.Network != "tcp" || event.Address != ln.Addr().String() {
		t.Fatalf("unexpected event target: %+v", event)
	}
	if event.Err != nil || event.LocalAddr == nil || event.RemoteAddr == nil {
		t.Fatalf("incomplete success event: %+v", event)
	}
	if event.StartedAt.IsZero() || event.Duration < 0 {
		t.Fatalf("invalid timing: %+v", event)
	}
}

func TestDialEventFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	events := make(chan tcp.DialEvent, 1)
	dialer := tcp.NewDialer(tcp.DialOptions{OnDial: func(event tcp.DialEvent) { events <- event }})
	conn, err := dialer.DialContext(ctx, "tcp", "127.0.0.1:1")
	if conn != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("conn=%v err=%v", conn, err)
	}
	event := <-events
	if !errors.Is(event.Err, context.Canceled) {
		t.Fatalf("event error = %v", event.Err)
	}
	if event.LocalAddr != nil || event.RemoteAddr != nil {
		t.Fatalf("failed dial reported addresses: %+v", event)
	}
}
