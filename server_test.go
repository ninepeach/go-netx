package netx_test

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/ninepeach/netx"
	"github.com/ninepeach/netx/tcp"
	"github.com/ninepeach/netx/udp"
)

func TestHighLevelTCPServerCommunicates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server, err := netx.NewTCPServer(ctx, "127.0.0.1:0", func(_ context.Context, conn net.Conn) error {
		_, err := io.Copy(conn, conn)
		return err
	}, netx.TCPServerOptions{MaxConnections: 8})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()

	conn, err := tcp.Dial(ctx, "tcp", server.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("high-level-tcp")); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, len("high-level-tcp"))
	if _, err := io.ReadFull(conn, response); err != nil {
		t.Fatal(err)
	}
	if string(response) != "high-level-tcp" {
		t.Fatalf("got %q", response)
	}
	conn.Close()
	cancel()
	shutdownCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := server.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestHighLevelUDPServerCommunicates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server, err := netx.NewUDPServer("127.0.0.1:0", func(_ context.Context, request netx.UDPRequest) ([]byte, error) {
		return append([]byte("reply:"), request.Payload...), nil
	}, netx.UDPServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	client, err := udp.Dial(ctx, "udp", server.Addr().String(), udp.ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	exchangeCtx, stopExchange := context.WithTimeout(ctx, time.Second)
	response, err := client.Exchange(exchangeCtx, []byte("high-level-udp"))
	stopExchange()
	if err != nil {
		t.Fatal(err)
	}
	if string(response) != "reply:high-level-udp" {
		t.Fatalf("got %q", response)
	}
	cancel()
	shutdownCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := server.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestHighLevelServerValidation(t *testing.T) {
	if _, err := netx.NewTCPServer(context.Background(), ":0", nil, netx.TCPServerOptions{}); err == nil {
		t.Fatal("expected nil TCP handler error")
	}
	if _, err := netx.NewUDPServer(":0", nil, netx.UDPServerOptions{}); err == nil {
		t.Fatal("expected nil UDP handler error")
	}
}
