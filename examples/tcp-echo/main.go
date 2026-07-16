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
	tcpService, err := netx.NewTCPServer(ctx, ":9000", func(_ context.Context, conn net.Conn) error {
		_, err := io.Copy(conn, conn)
		return err
	}, netx.TCPServerOptions{})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("TCP echo listening on %s", tcpService.Addr())
	if err := netx.NewServer(tcpService).LoopContext(ctx); err != nil {
		log.Fatal(err)
	}
}
