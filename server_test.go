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
	server := netx.NewTCPServer("127.0.0.1:0", netx.TCPServerOptions{MaxConnections: 8})
	server.OnConnect = func(_ context.Context, conn net.Conn) error {
		_, err := io.Copy(conn, conn)
		return err
	}
	done := make(chan error, 1)
	go func() { done <- server.LoopContext(ctx) }()
	<-server.Ready()
	if server.Addr() == nil {
		t.Fatalf("server failed to bind: %v", <-done)
	}

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
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestHighLevelUDPServerCommunicates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server := netx.NewUDPServer("127.0.0.1:0")
	server.OnPacket = func(_ context.Context, request netx.UDPRequest) ([]byte, error) {
		return append([]byte("reply:"), request.Payload...), nil
	}
	done := make(chan error, 1)
	go func() { done <- server.LoopContext(ctx) }()
	<-server.Ready()
	if server.Addr() == nil {
		t.Fatalf("server failed to bind: %v", <-done)
	}
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
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestHighLevelServerValidation(t *testing.T) {
	tcpServer := netx.NewTCPServer(":0")
	if err := tcpServer.LoopContext(context.Background()); err == nil {
		t.Fatal("expected nil TCP handler error")
	}
	udpServer := netx.NewUDPServer(":0")
	if err := udpServer.LoopContext(context.Background()); err == nil {
		t.Fatal("expected nil UDP handler error")
	}
}
