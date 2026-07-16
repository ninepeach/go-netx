package main

import (
	"context"
	"io"
	"log"
	"net"
	"os/signal"
	"syscall"

	"github.com/ninepeach/netx/tcp"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	srv, err := tcp.Listen(ctx, "tcp", ":9000", tcp.HandlerFunc(func(_ context.Context, conn net.Conn) error {
		_, err := io.Copy(conn, conn)
		return err
	}), tcp.ListenOptions{})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("TCP echo listening on %s", srv.Addr())
	if err := srv.Serve(ctx); err != nil {
		log.Fatal(err)
	}
}
