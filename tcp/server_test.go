package tcp_test

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/ninepeach/netx/tcp"
)

func TestServerEchoAndShutdown(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv, err := tcp.Listen(ctx, "tcp", "127.0.0.1:0", tcp.HandlerFunc(func(_ context.Context, conn net.Conn) error {
		_, err := io.Copy(conn, conn)
		return err
	}), tcp.ListenOptions{})
	if err != nil {
		t.Fatal(err)
	}

	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(ctx) }()

	conn, err := net.Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 5)
	if _, err = io.ReadFull(conn, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}
	_ = conn.Close()

	cancel()
	shutdownCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
	if err := <-serveDone; err != nil {
		t.Fatal(err)
	}
}

func TestServerReportsHandlerErrorAndClosesConnection(t *testing.T) {
	t.Parallel()
	want := errors.New("handler failed")
	reported := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, err := tcp.Listen(ctx, "tcp", "127.0.0.1:0", tcp.HandlerFunc(func(context.Context, net.Conn) error {
		return want
	}), tcp.ListenOptions{Server: tcp.Options{OnError: func(err error) { reported <- err }}})
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ctx) }()
	conn, err := net.Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	select {
	case got := <-reported:
		if !errors.Is(got, want) {
			t.Fatalf("reported %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("handler error not reported")
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	if _, err := conn.Read(buf); err == nil {
		t.Fatal("connection was not closed")
	}
	_ = srv.Close()
}

func TestNewServerValidation(t *testing.T) {
	if _, err := tcp.NewServer(nil, tcp.HandlerFunc(func(context.Context, net.Conn) error { return nil }), tcp.Options{}); err == nil {
		t.Fatal("expected nil listener error")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if _, err := tcp.NewServer(ln, nil, tcp.Options{}); err == nil {
		t.Fatal("expected nil handler error")
	}
}
