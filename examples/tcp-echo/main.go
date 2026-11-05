package main

import (
	"context"
	"io"
	"log"

	"github.com/ninepeach/go-netx"
	"github.com/ninepeach/go-netx/tcp"
)

func main() {
	ln, err := tcp.Listen(context.Background(), "tcp", ":9000", func(_ context.Context, c *tcp.Conn) {
		_, _ = io.Copy(c, c)
	}, tcp.Options{})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("TCP echo listening on %s", ln.Addr())
	if err := netx.NewServer(ln).Loop(); err != nil {
		log.Fatal(err)
	}
}
