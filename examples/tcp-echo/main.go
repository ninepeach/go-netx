package main

import (
	"context"
	"io"
	"log"
	"net"

	"github.com/ninepeach/go-netx"
)

func main() {
	s := netx.NewTCPServer(":9000")
	s.OnConnect = func(_ context.Context, conn net.Conn) error {
		_, err := io.Copy(conn, conn)
		return err
	}
	s.OnStart = func(context.Context) error {
		log.Printf("TCP echo listening on %s", s.Addr())
		return nil
	}
	if err := s.Loop(); err != nil {
		log.Fatal(err)
	}
}
