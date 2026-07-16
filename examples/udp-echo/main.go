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
	log.Printf("UDP echo listening on :9001")
	err := netx.ListenAndServeUDP(ctx, ":9001", func(_ context.Context, request netx.UDPRequest) ([]byte, error) {
		return request.Payload, nil
	})
	if err != nil {
		log.Fatal(err)
	}
}
