package udp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
)

type Packet struct {
	Payload    []byte
	RemoteAddr net.Addr
	LocalAddr  net.Addr
}

type Writer interface {
	WritePacket(context.Context, []byte, net.Addr) error
}
type Handler interface {
	ServeUDP(context.Context, Writer, Packet) error
}
type HandlerFunc func(context.Context, Writer, Packet) error

func (f HandlerFunc) ServeUDP(ctx context.Context, w Writer, p Packet) error { return f(ctx, w, p) }

type Options struct {
	MaxPacketSize int
	MaxHandlers   int
	OnError       func(error)
}

type Server struct {
	conn      net.PacketConn
	handler   Handler
	opts      Options
	mu        sync.Mutex
	serving   bool
	wg        sync.WaitGroup
	closeOnce sync.Once
	loopDone  chan struct{}
}

func NewServer(conn net.PacketConn, handler Handler, opts Options) (*Server, error) {
	if conn == nil {
		return nil, errors.New("udp: nil packet connection")
	}
	if handler == nil {
		return nil, errors.New("udp: nil handler")
	}
	if opts.MaxPacketSize < 0 || opts.MaxHandlers < 0 {
		return nil, errors.New("udp: limits must not be negative")
	}
	if opts.MaxPacketSize == 0 {
		opts.MaxPacketSize = 64 * 1024
	}
	return &Server{conn: conn, handler: handler, opts: opts, loopDone: make(chan struct{})}, nil
}

func Listen(network, address string, handler Handler, opts Options) (*Server, error) {
	conn, err := net.ListenPacket(network, address)
	if err != nil {
		return nil, err
	}
	srv, err := NewServer(conn, handler, opts)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return srv, nil
}

func (s *Server) Addr() net.Addr { return s.conn.LocalAddr() }

func (s *Server) Serve(ctx context.Context) error {
	s.mu.Lock()
	if s.serving {
		s.mu.Unlock()
		return errors.New("udp: Serve called more than once")
	}
	s.serving = true
	s.mu.Unlock()
	defer close(s.loopDone)
	stop := context.AfterFunc(ctx, func() { _ = s.Close() })
	defer stop()
	var sem chan struct{}
	if s.opts.MaxHandlers > 0 {
		sem = make(chan struct{}, s.opts.MaxHandlers)
	}
	buf := make([]byte, s.opts.MaxPacketSize)
	for {
		n, addr, err := s.conn.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("udp: read: %w", err)
		}
		if sem != nil {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return nil
			}
		}
		payload := append([]byte(nil), buf[:n]...)
		packet := Packet{Payload: payload, RemoteAddr: addr, LocalAddr: s.conn.LocalAddr()}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			if sem != nil {
				defer func() { <-sem }()
			}
			if err := s.handler.ServeUDP(ctx, packetWriter{s.conn}, packet); err != nil && s.opts.OnError != nil {
				s.opts.OnError(err)
			}
		}()
	}
}

func (s *Server) Close() error {
	var err error
	s.closeOnce.Do(func() { err = s.conn.Close() })
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error {
	_ = s.Close()
	s.mu.Lock()
	serving := s.serving
	loopDone := s.loopDone
	s.mu.Unlock()
	if serving {
		select {
		case <-loopDone:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type packetWriter struct{ conn net.PacketConn }

func (w packetWriter) WritePacket(ctx context.Context, payload []byte, addr net.Addr) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := w.conn.WriteTo(payload, addr)
	return err
}
