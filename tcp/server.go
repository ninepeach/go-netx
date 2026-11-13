// Package tcp serves accepted TCP connections with bounded admission and
// three-phase shutdown.
package tcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ninepeach/go-netx/socket"
)

var ErrClosed = errors.New("tcp: closed")

// Handler serves one accepted connection. The context is cancelled on abort.
type Handler func(context.Context, *Conn)

// Options configures listener sockets and admission limits.
type Options struct {
	Socket         socket.Options
	MaxConnections int
	MaxPerIP       int
	IPv6PrefixBits int
}

// Stats is a point-in-time server counter snapshot.
type Stats struct {
	Accepted      uint64
	Rejected      uint64
	RejectedPerIP uint64
	Active        uint64
	Closed        uint64
	AcceptErrors  uint64
}

// Conn is an accepted connection owned by Server.
type Conn struct {
	net.Conn
	id        uint64
	closeOnce sync.Once
	closeErr  error
	server    *Server
	ip        netip.Addr
	countedIP bool
}

// Close closes the accepted socket at most once, including during abort.
func (c *Conn) Close() error {
	c.closeOnce.Do(func() { c.closeErr = c.Conn.Close() })
	return c.closeErr
}

// ID is unique among connections accepted by this process.
func (c *Conn) ID() uint64 { return c.id }

// Draining closes when the server stops accepting.
func (c *Conn) Draining() <-chan struct{} { return c.server.draining }

// Server accepts TCP connections and coordinates their lifecycle.
type Server struct {
	listener net.Listener
	handler  Handler
	options  Options

	accepted      atomic.Uint64
	rejected      atomic.Uint64
	rejectedPerIP atomic.Uint64
	active        atomic.Uint64
	closed        atomic.Uint64
	acceptErrors  atomic.Uint64

	mu       sync.Mutex
	serving  bool
	stopped  bool
	early    bool
	conns    map[*Conn]struct{}
	perIP    map[netip.Addr]int
	ctx      context.Context
	abort    context.CancelFunc
	draining chan struct{}
	done     chan struct{}
	wg       sync.WaitGroup
	stopOnce sync.Once
}

var lastID atomic.Uint64

// Listen binds address and returns a server owning the listener.
func Listen(ctx context.Context, network, address string, handler Handler, options Options) (*Server, error) {
	lc := net.ListenConfig{Control: socket.ListenControl(options.Socket)}
	listener, err := lc.Listen(ctx, network, address)
	if err != nil {
		return nil, err
	}
	server, err := NewServer(listener, handler, options)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	return server, nil
}

// NewServer returns a server taking ownership of listener.
func NewServer(listener net.Listener, handler Handler, options Options) (*Server, error) {
	if listener == nil || handler == nil {
		return nil, errors.New("tcp: nil listener or handler")
	}
	if options.MaxConnections < 0 || options.MaxPerIP < 0 {
		return nil, errors.New("tcp: connection limits must not be negative")
	}
	if options.IPv6PrefixBits < 0 || options.IPv6PrefixBits > 128 {
		return nil, errors.New("tcp: IPv6PrefixBits must be within 0..128")
	}
	ctx, abort := context.WithCancel(context.Background())
	return &Server{
		listener: listener, handler: handler, options: options,
		conns: make(map[*Conn]struct{}), perIP: make(map[netip.Addr]int),
		ctx: ctx, abort: abort, draining: make(chan struct{}), done: make(chan struct{}),
	}, nil
}

// Addr returns the listener address.
func (s *Server) Addr() net.Addr { return s.listener.Addr() }

// Draining closes when Shutdown or Close stops acceptance.
func (s *Server) Draining() <-chan struct{} { return s.draining }

func (s *Server) Ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.serving && !s.stopped
}

// Serve accepts until stopped and waits for all handlers.
func (s *Server) Serve(parent context.Context) error {
	s.mu.Lock()
	if s.serving {
		stopped := s.stopped || s.early
		s.mu.Unlock()
		if stopped {
			return ErrClosed
		}
		return errors.New("tcp: Serve called more than once")
	}
	s.serving = true
	if s.stopped || s.early {
		early := s.early
		s.mu.Unlock()
		close(s.done)
		if early {
			return nil
		}
		return ErrClosed
	}
	s.ctx, s.abort = context.WithCancel(context.WithoutCancel(parent))
	s.mu.Unlock()
	defer close(s.done)

	stop := context.AfterFunc(parent, func() { _ = s.Close() })
	defer stop()

	err := s.accept()
	if err != nil {
		s.abortAll()
	}
	s.wg.Wait()
	s.abort()
	return err
}

func (s *Server) accept() error {
	var delay time.Duration
	for {
		raw, err := s.listener.Accept()
		if err != nil {
			if s.isStopped() {
				return nil
			}
			var temporary interface{ Temporary() bool }
			if errors.As(err, &temporary) && temporary.Temporary() {
				s.acceptErrors.Add(1)
				delay = nextDelay(delay)
				timer := time.NewTimer(delay)
				select {
				case <-timer.C:
				case <-s.draining:
					if !timer.Stop() {
						<-timer.C
					}
					return nil
				}
				continue
			}
			s.stopAccept()
			return fmt.Errorf("tcp: accept: %w", err)
		}
		delay = 0
		if !s.admit(raw) {
			_ = raw.Close()
			continue
		}
	}
}

func nextDelay(current time.Duration) time.Duration {
	if current == 0 {
		return 5 * time.Millisecond
	}
	current *= 2
	if current > time.Second {
		return time.Second
	}
	return current
}

func (s *Server) admit(raw net.Conn) bool {
	c := &Conn{Conn: raw, id: lastID.Add(1), server: s}
	if addr, ok := ipKey(raw.RemoteAddr(), s.options.IPv6PrefixBits); ok {
		c.ip, c.countedIP = addr, true
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return false
	}
	if max := s.options.MaxConnections; max > 0 && len(s.conns) >= max {
		s.rejected.Add(1)
		return false
	}
	if c.countedIP && s.options.MaxPerIP > 0 && s.perIP[c.ip] >= s.options.MaxPerIP {
		s.rejectedPerIP.Add(1)
		return false
	}
	s.conns[c] = struct{}{}
	if c.countedIP {
		s.perIP[c.ip]++
	}
	s.accepted.Add(1)
	s.active.Add(1)
	s.wg.Add(1)
	go s.serve(c)
	return true
}

func (s *Server) serve(c *Conn) {
	defer s.wg.Done()
	defer c.Close()
	defer s.release(c)
	s.handler(s.ctx, c)
}

func (s *Server) release(c *Conn) {
	s.mu.Lock()
	delete(s.conns, c)
	if c.countedIP {
		s.perIP[c.ip]--
		if s.perIP[c.ip] == 0 {
			delete(s.perIP, c.ip)
		}
	}
	s.mu.Unlock()
	s.active.Add(^uint64(0))
	s.closed.Add(1)
}

func ipKey(addr net.Addr, bits int) (netip.Addr, bool) {
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		return netip.Addr{}, false
	}
	ip, ok := netip.AddrFromSlice(tcpAddr.IP)
	if !ok {
		return netip.Addr{}, false
	}
	ip = ip.Unmap()
	if ip.Is6() {
		if bits == 0 {
			bits = 64
		}
		prefix, err := ip.Prefix(bits)
		if err != nil {
			return netip.Addr{}, false
		}
		ip = prefix.Masked().Addr()
	}
	return ip, true
}

func (s *Server) isStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

func (s *Server) stopAccept() {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.stopped = true
		s.mu.Unlock()
		_ = s.listener.Close()
		close(s.draining)
	})
}

func (s *Server) abortAll() {
	s.mu.Lock()
	s.abort()
	conns := make([]*Conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

// Shutdown stops acceptance, drains until ctx ends, then aborts.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if !s.serving {
		s.early = true
	}
	serving := s.serving
	s.mu.Unlock()
	s.stopAccept()
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

// Close stops acceptance and aborts every active connection.
func (s *Server) Close() error {
	s.stopAccept()
	s.abortAll()
	return nil
}

// Stats returns a consistent snapshot of independent atomic counters.
func (s *Server) Stats() Stats {
	return Stats{
		Accepted: s.accepted.Load(), Rejected: s.rejected.Load(),
		RejectedPerIP: s.rejectedPerIP.Load(), Active: s.active.Load(),
		Closed: s.closed.Load(), AcceptErrors: s.acceptErrors.Load(),
	}
}
