package tcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/ninepeach/go-netx/socket"
)

var ErrServerClosed = errors.New("tcp: server closed")

type ErrorHandler func(error)

type Options struct {
	MaxConnections int
	OnError        ErrorHandler
	OnOpen         func(context.Context, net.Conn) error
	OnClose        func(net.Conn)
}

type ListenOptions struct {
	Server Options
	Socket socket.Options
}

type Server struct {
	listener net.Listener
	handler  Handler
	opts     Options

	mu        sync.Mutex
	serving   bool
	closed    bool
	conns     map[net.Conn]struct{}
	wg        sync.WaitGroup
	closeOnce sync.Once
	loopDone  chan struct{}
}

func NewServer(listener net.Listener, handler Handler, opts Options) (*Server, error) {
	if listener == nil {
		return nil, errors.New("tcp: nil listener")
	}
	if handler == nil {
		return nil, errors.New("tcp: nil handler")
	}
	if opts.MaxConnections < 0 {
		return nil, errors.New("tcp: MaxConnections must not be negative")
	}
	return &Server{listener: listener, handler: handler, opts: opts, conns: make(map[net.Conn]struct{}), loopDone: make(chan struct{})}, nil
}

func Listen(ctx context.Context, network, address string, handler Handler, opts ListenOptions) (*Server, error) {
	lc := socket.ListenConfig(opts.Socket)
	ln, err := lc.Listen(ctx, network, address)
	if err != nil {
		return nil, err
	}
	srv, err := NewServer(ln, handler, opts.Server)
	if err != nil {
		_ = ln.Close()
		return nil, err
	}
	return srv, nil
}

func (s *Server) Addr() net.Addr { return s.listener.Addr() }

func (s *Server) Serve(ctx context.Context) error {
	s.mu.Lock()
	if s.serving {
		s.mu.Unlock()
		return errors.New("tcp: Serve called more than once")
	}
	if s.closed {
		s.mu.Unlock()
		return ErrServerClosed
	}
	s.serving = true
	s.mu.Unlock()
	defer close(s.loopDone)

	stop := context.AfterFunc(ctx, func() { _ = s.Close() })
	defer stop()

	var delay time.Duration
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil || s.isClosed() {
				return nil
			}
			var temporary interface{ Temporary() bool }
			if errors.As(err, &temporary) && temporary.Temporary() {
				if delay == 0 {
					delay = 5 * time.Millisecond
				} else {
					delay *= 2
				}
				if delay > time.Second {
					delay = time.Second
				}
				s.report(fmt.Errorf("tcp: accept: %w", err))
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil
				case <-timer.C:
				}
				continue
			}
			return fmt.Errorf("tcp: accept: %w", err)
		}
		delay = 0
		if !s.track(conn) {
			_ = conn.Close()
			s.report(errors.New("tcp: connection limit reached"))
			continue
		}
		s.wg.Add(1)
		go s.serveConn(ctx, conn)
	}
}

func (s *Server) serveConn(ctx context.Context, conn net.Conn) {
	defer s.wg.Done()
	defer s.untrack(conn)
	defer conn.Close()
	if s.opts.OnClose != nil {
		defer s.opts.OnClose(conn)
	}
	if s.opts.OnOpen != nil {
		if err := s.opts.OnOpen(ctx, conn); err != nil {
			s.report(err)
			return
		}
	}
	if err := s.handler.ServeTCP(ctx, conn); err != nil {
		s.report(err)
	}
}

func (s *Server) track(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || (s.opts.MaxConnections > 0 && len(s.conns) >= s.opts.MaxConnections) {
		return false
	}
	s.conns[conn] = struct{}{}
	return true
}

func (s *Server) untrack(conn net.Conn) { s.mu.Lock(); delete(s.conns, conn); s.mu.Unlock() }
func (s *Server) isClosed() bool        { s.mu.Lock(); defer s.mu.Unlock(); return s.closed }
func (s *Server) report(err error) {
	if s.opts.OnError != nil {
		s.opts.OnError(err)
	}
}

func (s *Server) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		err = s.listener.Close()
	})
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
			s.closeActiveConnections()
			return ctx.Err()
		}
	}
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		s.closeActiveConnections()
		return ctx.Err()
	}
}

func (s *Server) closeActiveConnections() {
	s.mu.Lock()
	connections := make([]net.Conn, 0, len(s.conns))
	for conn := range s.conns {
		connections = append(connections, conn)
	}
	s.mu.Unlock()
	for _, conn := range connections {
		_ = conn.Close()
	}
}
