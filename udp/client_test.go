package udp_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/ninepeach/netx/udp"
)

func TestClientExchangeAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server, err := udp.Listen("udp", "127.0.0.1:0", udp.HandlerFunc(func(ctx context.Context, writer udp.Writer, packet udp.Packet) error {
		return writer.WritePacket(ctx, packet.Payload, packet.RemoteAddr)
	}), udp.Options{})
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(ctx) }()
	defer func() { cancel(); _ = server.Close() }()

	client, err := udp.Dial(ctx, "udp", server.Addr().String(), udp.ClientOptions{MaxPacketSize: 128})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	exchangeCtx, stop := context.WithTimeout(ctx, time.Second)
	got, err := client.Exchange(exchangeCtx, []byte("hello"))
	stop()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}

	cancelled, stopCancelled := context.WithCancel(context.Background())
	stopCancelled()
	if _, err := client.Exchange(cancelled, []byte("ignored")); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

func TestNewClientValidation(t *testing.T) {
	if _, err := udp.NewClient(nil, udp.ClientOptions{}); err == nil {
		t.Fatal("expected nil connection error")
	}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	if _, err := udp.NewClient(left, udp.ClientOptions{MaxPacketSize: -1}); err == nil {
		t.Fatal("expected invalid size error")
	}
}
