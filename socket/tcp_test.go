package socket_test

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/ninepeach/go-netx/socket"
)

func TestTCPStandardConn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	lc := net.ListenConfig{Control: socket.ListenControl(socket.Options{FastOpen: true})}
	l, err := lc.Listen(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.(*net.TCPListener).SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	d := net.Dialer{ControlContext: socket.DialControl(socket.Options{})}
	client, err := d.DialContext(ctx, "tcp4", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	cancel()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("accept: %v", err)
	}
	for _, c := range []net.Conn{client, server} {
		if err := c.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.Write([]byte("request")); err != nil {
		t.Fatal(err)
	}
	if err := client.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(server)
	if err != nil || string(data) != "request" {
		t.Fatalf("request: %q, %v", data, err)
	}
	if _, err := server.Write([]byte("reply")); err != nil {
		t.Fatal(err)
	}
	if err := server.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	data, err = io.ReadAll(client)
	if err != nil || string(data) != "reply" {
		t.Fatalf("reply: %q, %v", data, err)
	}

	client, err = d.DialContext(ctx, "tcp4", l.Addr().String())
	if client != nil {
		client.Close()
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled dial: %v", err)
	}
}

func TestTCPReadDeadline(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	c, err := net.DialTimeout("tcp4", l.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.SetReadDeadline(time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Read(make([]byte, 1)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("read: %v", err)
	}
	if err := c.(*net.TCPConn).CloseRead(); err != nil {
		t.Fatal(err)
	}
}
