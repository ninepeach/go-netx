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
	log.Printf("TCP echo listening on :9000")
	err := netx.ListenAndServeTCP(ctx, ":9000", func(_ context.Context, conn net.Conn) error {
		_, err := io.Copy(conn, conn)
		return err
	})
	if err != nil {
		log.Fatal(err)
	}
}
