package netx

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/ninepeach/go-netx/socket"
	"github.com/ninepeach/go-netx/tcp"
	"github.com/ninepeach/go-netx/udp"
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

// NewTCPService binds address and returns the lower-level TCP service. Most
// applications should use NewTCPServer.
func NewTCPService(ctx context.Context, address string, handler TCPHandler, opts TCPServerOptions) (*tcp.Server, error) {
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

// TCPServer is a high-level, overridable TCP server.
type TCPServer struct {
	*Server
	Address   string
	Options   TCPServerOptions
	OnConnect TCPHandler

	prepareOnce sync.Once
	prepareErr  error
	service     *tcp.Server
	ready       chan struct{}
}

// NewTCPServer creates a TCP server. Set OnConnect, then call Loop or
// LoopContext. Network binding is delayed until the loop starts.
func NewTCPServer(address string, options ...TCPServerOptions) *TCPServer {
	config, err := oneTCPOptions(options)
	runtime := NewServer()
	runtime.ShutdownTimeout = config.ShutdownTimeout
	if runtime.ShutdownTimeout == 0 {
		runtime.ShutdownTimeout = 30 * time.Second
	}
	return &TCPServer{Server: runtime, Address: address, Options: config, prepareErr: err, ready: make(chan struct{})}
}

func (s *TCPServer) Loop() error {
	if err := s.prepare(context.Background()); err != nil {
		return err
	}
	return s.Server.Loop()
}

func (s *TCPServer) LoopContext(ctx context.Context) error {
	if err := s.prepare(ctx); err != nil {
		return err
	}
	return s.Server.LoopContext(ctx)
}

func (s *TCPServer) Addr() net.Addr {
	if s.service == nil {
		return nil
	}
	return s.service.Addr()
}

// Ready is closed after network binding succeeds or fails.
func (s *TCPServer) Ready() <-chan struct{} { return s.ready }

func (s *TCPServer) prepare(ctx context.Context) error {
	s.prepareOnce.Do(func() {
		defer close(s.ready)
		if s.prepareErr != nil {
			return
		}
		if s.OnConnect == nil {
			s.prepareErr = errors.New("netx: TCP OnConnect is required")
			return
		}
		s.service, s.prepareErr = NewTCPService(ctx, s.Address, s.OnConnect, s.Options)
		if s.prepareErr == nil {
			s.Server.Service = s.service
		}
	})
	return s.prepareErr
}

// ListenAndServeTCP creates a production-ready TCP server, serves until ctx is
// cancelled, then waits for active handlers to finish. Passing zero options
// selects portable defaults.
func ListenAndServeTCP(ctx context.Context, address string, handler TCPHandler, opts ...TCPServerOptions) error {
	config, err := oneTCPOptions(opts)
	if err != nil {
		return err
	}
	server, err := NewTCPService(ctx, address, handler, config)
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

// NewUDPService binds address and returns the lower-level UDP service. Most
// applications should use NewUDPServer.
func NewUDPService(address string, handler UDPHandler, opts UDPServerOptions) (*udp.Server, error) {
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

// UDPServer is a high-level, overridable UDP server.
type UDPServer struct {
	*Server
	Address  string
	Options  UDPServerOptions
	OnPacket UDPHandler

	prepareOnce sync.Once
	prepareErr  error
	service     *udp.Server
	ready       chan struct{}
}

// NewUDPServer creates a UDP server. Set OnPacket, then call Loop or
// LoopContext. Network binding is delayed until the loop starts.
func NewUDPServer(address string, options ...UDPServerOptions) *UDPServer {
	config, err := oneUDPOptions(options)
	runtime := NewServer()
	runtime.ShutdownTimeout = config.ShutdownTimeout
	if runtime.ShutdownTimeout == 0 {
		runtime.ShutdownTimeout = 30 * time.Second
	}
	return &UDPServer{Server: runtime, Address: address, Options: config, prepareErr: err, ready: make(chan struct{})}
}

func (s *UDPServer) Loop() error {
	if err := s.prepare(); err != nil {
		return err
	}
	return s.Server.Loop()
}

func (s *UDPServer) LoopContext(ctx context.Context) error {
	if err := s.prepare(); err != nil {
		return err
	}
	return s.Server.LoopContext(ctx)
}

func (s *UDPServer) Addr() net.Addr {
	if s.service == nil {
		return nil
	}
	return s.service.Addr()
}

// Ready is closed after network binding succeeds or fails.
func (s *UDPServer) Ready() <-chan struct{} { return s.ready }

func (s *UDPServer) prepare() error {
	s.prepareOnce.Do(func() {
		defer close(s.ready)
		if s.prepareErr != nil {
			return
		}
		if s.OnPacket == nil {
			s.prepareErr = errors.New("netx: UDP OnPacket is required")
			return
		}
		s.service, s.prepareErr = NewUDPService(s.Address, s.OnPacket, s.Options)
		if s.prepareErr == nil {
			s.Server.Service = s.service
		}
	})
	return s.prepareErr
}

// ListenAndServeUDP creates a UDP server, serves until ctx is cancelled, and
// waits for active handlers to finish.
func ListenAndServeUDP(ctx context.Context, address string, handler UDPHandler, opts ...UDPServerOptions) error {
	config, err := oneUDPOptions(opts)
	if err != nil {
		return err
	}
	server, err := NewUDPService(address, handler, config)
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
