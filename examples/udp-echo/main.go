package main

import (
	"context"
	"log"

	"github.com/ninepeach/go-netx"
)

func main() {
	s := netx.NewUDPServer(":9001")
	s.OnPacket = func(_ context.Context, request netx.UDPRequest) ([]byte, error) {
		return request.Payload, nil
	}
	s.OnStart = func(context.Context) error {
		log.Printf("UDP echo listening on %s", s.Addr())
		return nil
	}
	if err := s.Loop(); err != nil {
		log.Fatal(err)
	}
}
