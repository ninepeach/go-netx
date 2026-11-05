// Package tcp is a stream transport: an accept loop with admission control,
// one goroutine per connection, and three-phase shutdown.
//
// Shutdown runs StopAccept → Drain → Abort. StopAccept closes the listener
// and closes Draining so long-lived handlers can ask clients to move. Drain
// waits for handlers to return. Abort, when the shutdown context ends,
// cancels the handler context and closes every connection.
package tcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ninepeach/go-netx/internal/counter"
	"github.com/ninepeach/go-netx/limit"
	"github.com/ninepeach/go-netx/socket"
)

// ErrClosed is returned by Serve after Close or Shutdown.
var ErrClosed = errors.New("tcp: closed")

// Conn is an accepted connection. The server owns it and closes it after
// the Handler returns.
type Conn struct {
	net.Conn
	id  uint64
	srv *Server
}

// ID is unique among connections accepted by this process.
func (c *Conn) ID() uint64 { return c.id }

// Draining is closed when the server stops accepting and starts draining.
func (c *Conn) Draining() <-chan struct{} { return c.srv.draining }

// Handler serves one connection. ctx is cancelled only on Abort, not when
// draining starts; watch c.Draining() for that. Panics are not recovered.
type Handler func(ctx context.Context, c *Conn)

// Options configures a Server.
type Options struct {
	// Admit is consulted for every accepted connection; rejected
	// connections are closed immediately and counted. Nil admits all.
	Admit  limit.Limiter
	Socket socket.Options
}

// Stats is a point-in-time snapshot.
type Stats struct {
	Accepted     uint64
	Rejected     uint64 // refused by Admit
	Active       uint64
	Closed       uint64
	AcceptErrors uint64 // temporary accept failures, retried with backoff
}

var lastConnID atomic.Uint64

// Server accepts TCP connections. It implements the netx Service contract.
type Server struct {
	ln      net.Listener
	handler Handler
	opts    Options

	accepted, rejected, active, closed, acceptErrors counter.Counter

	mu       sync.Mutex
	serving  bool
	stopped  bool // listener closed
	conns    map[*Conn]struct{}
	ctx      context.Context // handler context, cancelled on Abort
	abort    context.CancelFunc
	draining chan struct{}
	done     chan struct{} // closed when Serve returns
	wg       sync.WaitGroup
	stopOnce sync.Once
}

// Listen binds network ("tcp", "tcp4", "tcp6") at address.
func Listen(ctx context.Context, network, address string, h Handler, o Options) (*Server, error) {
	lc := socket.ListenConfig(o.Socket)
	ln, err := lc.Listen(ctx, network, address)
	if err != nil {
		return nil, err
	}
	s, err := NewServer(ln, h, o)
	if err != nil {
		_ = ln.Close()
		return nil, err
	}
	return s, nil
}

// NewServer serves an existing listener, which the server then owns.
func NewServer(ln net.Listener, h Handler, o Options) (*Server, error) {
	if ln == nil || h == nil {
		return nil, errors.New("tcp: nil listener or handler")
	}
	ctx, abort := context.WithCancel(context.Background())
	return &Server{
		ln: ln, handler: h, opts: o, conns: make(map[*Conn]struct{}),
		ctx: ctx, abort: abort, draining: make(chan struct{}), done: make(chan struct{}),
	}, nil
}

func (s *Server) Addr() net.Addr { return s.ln.Addr() }

// Draining is closed when the server stops accepting.
func (s *Server) Draining() <-chan struct{} { return s.draining }

// Serve accepts until Shutdown or Close, or until ctx is cancelled, which
// aborts. It returns after every handler has returned. Values in ctx are
// visible to handlers.
func (s *Server) Serve(ctx context.Context) error {
	s.mu.Lock()
	switch {
	case s.stopped:
		s.mu.Unlock()
		return ErrClosed
	case s.serving:
		s.mu.Unlock()
		return errors.New("tcp: Serve called more than once")
	}
	s.serving = true
	s.ctx, s.abort = context.WithCancel(context.WithoutCancel(ctx))
	s.mu.Unlock()
	defer close(s.done)

	stop := context.AfterFunc(ctx, func() { _ = s.Close() })
	defer stop()

	err := s.acceptLoop()
	s.wg.Wait()
	s.abort()
	return err
}

func (s *Server) acceptLoop() error {
	var backoff time.Duration
	for {
		nc, err := s.ln.Accept()
		if err != nil {
			if s.isStopped() {
				return nil
			}
			var temp interface{ Temporary() bool }
			if errors.As(err, &temp) && temp.Temporary() {
				s.acceptErrors.Inc()
				backoff = min(max(backoff*2, 5*time.Millisecond), time.Second)
				time.Sleep(backoff)
				continue
			}
			s.stopAccept()
			return fmt.Errorf("tcp: accept: %w", err)
		}
		backoff = 0
		if s.opts.Admit != nil && !s.opts.Admit.TryAcquire() {
			s.rejected.Inc()
			_ = nc.Close()
			continue
		}
		c := &Conn{Conn: nc, id: lastConnID.Add(1), srv: s}
		if !s.track(c) {
			s.release(c)
			_ = nc.Close()
			return nil
		}
		s.accepted.Inc()
		s.active.Inc()
		s.wg.Add(1)
		go s.serveConn(c)
	}
}

func (s *Server) serveConn(c *Conn) {
	defer s.wg.Done()
	defer s.release(c)
	defer s.untrack(c)
	defer c.Close()
	if tc, ok := c.Conn.(*net.TCPConn); ok {
		_ = socket.SetBuffers(tc, s.opts.Socket)
	}
	s.handler(s.ctx, c)
}

func (s *Server) release(*Conn) {
	if s.opts.Admit != nil {
		s.opts.Admit.Release()
	}
}

func (s *Server) track(c *Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return false
	}
	s.conns[c] = struct{}{}
	return true
}

func (s *Server) untrack(c *Conn) {
	s.mu.Lock()
	delete(s.conns, c)
	s.mu.Unlock()
	s.active.Dec()
	s.closed.Inc()
}

func (s *Server) isStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

// stopAccept closes the listener and signals Draining.
func (s *Server) stopAccept() {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.stopped = true
		s.mu.Unlock()
		_ = s.ln.Close()
		close(s.draining)
	})
}

// abortAll cancels handlers and closes every connection.
func (s *Server) abortAll() {
	s.mu.Lock()
	s.abort()
	conns := make([]*Conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	for _, c := range conns {
		_ = c.Conn.Close()
	}
}

// Shutdown stops accepting, waits for handlers until ctx is done, then
// aborts the rest and returns ctx.Err().
func (s *Server) Shutdown(ctx context.Context) error {
	s.stopAccept()
	s.mu.Lock()
	serving := s.serving
	s.mu.Unlock()
	if !serving {
		return nil
	}
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		s.abortAll()
		return ctx.Err()
	}
}

// Close stops accepting and aborts every connection without waiting.
func (s *Server) Close() error {
	s.stopAccept()
	s.abortAll()
	return nil
}

func (s *Server) Stats() Stats {
	return Stats{
		Accepted: s.accepted.Load(), Rejected: s.rejected.Load(),
		Active: s.active.Load(), Closed: s.closed.Load(),
		AcceptErrors: s.acceptErrors.Load(),
	}
}
