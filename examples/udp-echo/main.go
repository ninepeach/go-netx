package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/ninepeach/netx/udp"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	srv, err := udp.Listen("udp", ":9001", udp.HandlerFunc(func(ctx context.Context, writer udp.Writer, packet udp.Packet) error {
		return writer.WritePacket(ctx, packet.Payload, packet.RemoteAddr)
	}), udp.Options{})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("UDP echo listening on %s", srv.Addr())
	if err := srv.Serve(ctx); err != nil {
		log.Fatal(err)
	}
}
