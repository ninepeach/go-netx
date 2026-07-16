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
	s := netx.NewUDPServer(":9001")
	s.OnPacket = func(_ context.Context, request netx.UDPRequest) ([]byte, error) {
		return request.Payload, nil
	}
	s.OnStart = func(context.Context) error {
		log.Printf("UDP echo listening on %s", s.Addr())
		return nil
	}
	if err := s.LoopContext(ctx); err != nil {
		log.Fatal(err)
	}
}
