package main

import (
	"context"
	"io"
	"log"
	"net"
	"os/signal"
	"syscall"

	"github.com/ninepeach/netx"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	s := netx.NewTCPServer(":9000")
	s.OnConnect = func(_ context.Context, conn net.Conn) error {
		_, err := io.Copy(conn, conn)
		return err
	}
	s.OnStart = func(context.Context) error {
		log.Printf("TCP echo listening on %s", s.Addr())
		return nil
	}
	if err := s.LoopContext(ctx); err != nil {
		log.Fatal(err)
	}
}
