package udp_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/ninepeach/netx/udp"
)

func TestServerEcho(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, err := udp.Listen("udp", "127.0.0.1:0", udp.HandlerFunc(func(ctx context.Context, w udp.Writer, p udp.Packet) error {
		return w.WritePacket(ctx, p.Payload, p.RemoteAddr)
	}), udp.Options{})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()

	conn, err := net.Dial("udp", srv.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := conn.Read(buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "ping" {
		t.Fatalf("got %q", buf)
	}

	cancel()
	shutdownCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestNewServerValidation(t *testing.T) {
	if _, err := udp.NewServer(nil, udp.HandlerFunc(func(context.Context, udp.Writer, udp.Packet) error { return nil }), udp.Options{}); err == nil {
		t.Fatal("expected error")
	}
}
