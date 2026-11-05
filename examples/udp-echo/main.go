package main

import (
	"context"
	"log"

	"github.com/ninepeach/go-netx"
	"github.com/ninepeach/go-netx/udp"
)

func main() {
	conn, err := udp.Listen(context.Background(), "udp", ":9001", func(c *udp.Conn, p *udp.Packet) {
		_ = c.Reply(p, p.Data)
	}, udp.Options{})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("UDP echo listening on %s", conn.LocalAddr())
	if err := netx.NewServer(conn).Loop(); err != nil {
		log.Fatal(err)
	}
}
