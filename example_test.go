package netx_test

import (
	"context"
	"io"
	"net"

	"github.com/ninepeach/go-netx/tcp"
	"github.com/ninepeach/go-netx/udp"
)

func Example_tcpServer() {
	ctx := context.Background()
	srv, err := tcp.Listen(ctx, "tcp", "127.0.0.1:9000", tcp.HandlerFunc(func(_ context.Context, conn net.Conn) error {
		_, err := io.Copy(conn, conn)
		return err
	}), tcp.ListenOptions{})
	if err != nil {
		return
	}
	defer srv.Close()
	// In a real program: err = srv.Serve(ctx)
}

func Example_udpServer() {
	srv, err := udp.Listen("udp", "127.0.0.1:9001", udp.HandlerFunc(func(ctx context.Context, writer udp.Writer, packet udp.Packet) error {
		return writer.WritePacket(ctx, packet.Payload, packet.RemoteAddr)
	}), udp.Options{})
	if err != nil {
		return
	}
	defer srv.Close()
	// In a real program: err = srv.Serve(context.Background())
}
