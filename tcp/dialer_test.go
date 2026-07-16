package tcp_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/ninepeach/netx/tcp"
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
