package netx

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/ninepeach/netx/socket"
	"github.com/ninepeach/netx/tcp"
	"github.com/ninepeach/netx/udp"
)

// TCPHandler handles one accepted TCP connection. The server owns conn and
// closes it after the handler returns.
type TCPHandler func(context.Context, net.Conn) error

// TCPServerOptions configures the high-level TCP server.
type TCPServerOptions struct {
	Network         string
	MaxConnections  int
	ShutdownTimeout time.Duration
	Socket          socket.Options
	OnError         func(error)
}

// NewTCPServer binds address and returns a server ready to Serve. Most callers
// can use ListenAndServeTCP instead.
func NewTCPServer(ctx context.Context, address string, handler TCPHandler, opts TCPServerOptions) (*tcp.Server, error) {
	if handler == nil {
		return nil, errors.New("netx: nil TCP handler")
	}
	network := opts.Network
	if network == "" {
		network = "tcp"
	}
	return tcp.Listen(ctx, network, address, tcp.HandlerFunc(handler), tcp.ListenOptions{
		Server: tcp.Options{MaxConnections: opts.MaxConnections, OnError: opts.OnError},
		Socket: opts.Socket,
	})
}

// ListenAndServeTCP creates a production-ready TCP server, serves until ctx is
// cancelled, then waits for active handlers to finish. Passing zero options
// selects portable defaults.
func ListenAndServeTCP(ctx context.Context, address string, handler TCPHandler, opts ...TCPServerOptions) error {
	config, err := oneTCPOptions(opts)
	if err != nil {
		return err
	}
	server, err := NewTCPServer(ctx, address, handler, config)
	if err != nil {
		return err
	}
	serveErr := server.Serve(ctx)
	shutdownCtx := context.Background()
	stop := func() {}
	if config.ShutdownTimeout > 0 {
		shutdownCtx, stop = context.WithTimeout(shutdownCtx, config.ShutdownTimeout)
	}
	defer stop()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	return serveErr
}

func oneTCPOptions(options []TCPServerOptions) (TCPServerOptions, error) {
	if len(options) > 1 {
		return TCPServerOptions{}, errors.New("netx: expected at most one TCPServerOptions")
	}
	if len(options) == 1 {
		return options[0], nil
	}
	return TCPServerOptions{}, nil
}

// UDPRequest is one received datagram.
type UDPRequest struct {
	Payload    []byte
	RemoteAddr net.Addr
	LocalAddr  net.Addr
}

// UDPHandler returns the datagram to send back to the request's source. A nil
// response sends nothing.
type UDPHandler func(context.Context, UDPRequest) ([]byte, error)

type UDPServerOptions struct {
	Network         string
	MaxPacketSize   int
	MaxHandlers     int
	ShutdownTimeout time.Duration
	OnError         func(error)
}

// NewUDPServer binds address and returns a server ready to Serve.
func NewUDPServer(address string, handler UDPHandler, opts UDPServerOptions) (*udp.Server, error) {
	if handler == nil {
		return nil, errors.New("netx: nil UDP handler")
	}
	network := opts.Network
	if network == "" {
		network = "udp"
	}
	return udp.Listen(network, address, udp.HandlerFunc(func(ctx context.Context, writer udp.Writer, packet udp.Packet) error {
		response, err := handler(ctx, UDPRequest{
			Payload: packet.Payload, RemoteAddr: packet.RemoteAddr, LocalAddr: packet.LocalAddr,
		})
		if err != nil || response == nil {
			return err
		}
		return writer.WritePacket(ctx, response, packet.RemoteAddr)
	}), udp.Options{MaxPacketSize: opts.MaxPacketSize, MaxHandlers: opts.MaxHandlers, OnError: opts.OnError})
}

// ListenAndServeUDP creates a UDP server, serves until ctx is cancelled, and
// waits for active handlers to finish.
func ListenAndServeUDP(ctx context.Context, address string, handler UDPHandler, opts ...UDPServerOptions) error {
	config, err := oneUDPOptions(opts)
	if err != nil {
		return err
	}
	server, err := NewUDPServer(address, handler, config)
	if err != nil {
		return err
	}
	serveErr := server.Serve(ctx)
	shutdownCtx := context.Background()
	stop := func() {}
	if config.ShutdownTimeout > 0 {
		shutdownCtx, stop = context.WithTimeout(shutdownCtx, config.ShutdownTimeout)
	}
	defer stop()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	return serveErr
}

func oneUDPOptions(options []UDPServerOptions) (UDPServerOptions, error) {
	if len(options) > 1 {
		return UDPServerOptions{}, errors.New("netx: expected at most one UDPServerOptions")
	}
	if len(options) == 1 {
		return options[0], nil
	}
	return UDPServerOptions{}, nil
}
