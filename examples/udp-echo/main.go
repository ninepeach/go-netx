package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/ninepeach/netx"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	udpService, err := netx.NewUDPServer(":9001", func(_ context.Context, request netx.UDPRequest) ([]byte, error) {
		return request.Payload, nil
	}, netx.UDPServerOptions{})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("UDP echo listening on %s", udpService.Addr())
	if err := netx.NewServer(udpService).LoopContext(ctx); err != nil {
		log.Fatal(err)
	}
}
