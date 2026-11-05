package netx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var (
	ErrAlreadyRunning = errors.New("netx: server is already running or has stopped")
	ErrNotRunning     = errors.New("netx: server is not running")
	// ErrAbortTimeout means a service's Serve did not return within
	// AbortGrace after its context was cancelled.
	ErrAbortTimeout = errors.New("netx: service did not stop after abort")
)

// AbortGrace bounds how long Server waits for Serve to return after
// cancelling its context, so a stuck service cannot hang process exit.
const AbortGrace = time.Second

// Service is the lifecycle contract.
//
// Serve blocks until the service has fully stopped. Its context is
// cancelled only to abort: the service must then close everything and
// return promptly.
//
// Shutdown stops accepting new work immediately, drains in-flight work until
// its context is done, then aborts what remains and returns ctx.Err().
type Service interface {
	Serve(context.Context) error
	Shutdown(context.Context) error
}

// ServiceFuncs adapts functions to Service. A nil ShutdownFunc does nothing.
type ServiceFuncs struct {
	ServeFunc    func(context.Context) error
	ShutdownFunc func(context.Context) error
}

func (f ServiceFuncs) Serve(ctx context.Context) error { return f.ServeFunc(ctx) }

func (f ServiceFuncs) Shutdown(ctx context.Context) error {
	if f.ShutdownFunc == nil {
		return nil
	}
	return f.ShutdownFunc(ctx)
}

// Group runs services together. Serve returns when all have returned; if
// one fails, the others are aborted. Shutdown shuts all down concurrently.
func Group(services ...Service) Service { return group(services) }

type group []Service

func (g group) Serve(ctx context.Context) error {
	ctx, abort := context.WithCancel(ctx)
	defer abort()
	errs := make([]error, len(g))
	var wg sync.WaitGroup
	for i, s := range g {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if errs[i] = s.Serve(ctx); errs[i] != nil {
				abort()
			}
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

func (g group) Shutdown(ctx context.Context) error {
	errs := make([]error, len(g))
	var wg sync.WaitGroup
	for i, s := range g {
		wg.Add(1)
		go func() { defer wg.Done(); errs[i] = s.Shutdown(ctx) }()
	}
	wg.Wait()
	return errors.Join(errs...)
}

// State is a Server's position in new → starting → running → stopping → stopped.
type State uint8

const (
	StateNew State = iota
	StateStarting
	StateRunning
	StateStopping
	StateStopped
)

func (s State) String() string {
	switch s {
	case StateNew:
		return "new"
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StateStopping:
		return "stopping"
	case StateStopped:
		return "stopped"
	}
	return "unknown"
}

// Server runs one Service from start to graceful stop. It is one-shot.
type Server struct {
	Service Service

	// OnStart runs before Serve; an error stops the server.
	OnStart func(context.Context) error
	// OnStop runs after the service has stopped.
	OnStop func(context.Context) error
	// OnError receives the final error, if any.
	OnError func(error)

	// Signals that start shutdown in Loop. Default SIGINT and SIGTERM.
	Signals []os.Signal
	// ShutdownTimeout bounds the drain phase. Zero means no bound.
	ShutdownTimeout time.Duration

	mu     sync.Mutex
	state  State
	stop   context.CancelFunc
	done   chan struct{}
	result error
}

// NewServer returns a server for s with a 30 second shutdown timeout.
func NewServer(s Service) *Server {
	return &Server{Service: s, ShutdownTimeout: 30 * time.Second, done: make(chan struct{})}
}

func (s *Server) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Loop runs until a signal in Signals arrives, then shuts down gracefully.
func (s *Server) Loop() error {
	signals := s.Signals
	if len(signals) == 0 {
		signals = []os.Signal{syscall.SIGINT, syscall.SIGTERM}
	}
	ctx, stop := signal.NotifyContext(context.Background(), signals...)
	defer stop()
	return s.LoopContext(ctx)
}

// LoopContext runs until ctx is done, Stop or Shutdown is called, or the
// service returns, then shuts down gracefully. Cancelling ctx starts the
// drain; it does not abort the service directly.
func (s *Server) LoopContext(parent context.Context) (result error) {
	if s.Service == nil {
		return errors.New("netx: Service is required")
	}
	stopCtx, err := s.begin(parent)
	if err != nil {
		return err
	}
	defer func() { s.finish(result) }()

	// The service context outlives the stop signal and is cancelled only
	// to abort, after the drain.
	runCtx, abort := context.WithCancel(context.WithoutCancel(parent))
	defer abort()

	if s.OnStart != nil {
		if err := protect("start", func() error { return s.OnStart(runCtx) }); err != nil {
			ctx, cancel := s.shutdownContext()
			defer cancel()
			return errors.Join(err, s.Service.Shutdown(ctx), s.onStop(ctx))
		}
	}
	s.setState(StateRunning)

	served := make(chan error, 1)
	go func() { served <- protect("serve", func() error { return s.Service.Serve(runCtx) }) }()

	var serveErr error
	returned := false
	select {
	case serveErr = <-served:
		returned = true
	case <-stopCtx.Done():
	}

	s.setState(StateStopping)
	ctx, cancel := s.shutdownContext()
	defer cancel()
	shutdownErr := protect("shutdown", func() error { return s.Service.Shutdown(ctx) })
	if !returned {
		abort()
		select {
		case serveErr = <-served:
		case <-time.After(AbortGrace):
			serveErr = ErrAbortTimeout
		}
	}
	result = errors.Join(serveErr, ignoreCanceled(shutdownErr), s.onStop(ctx))
	if result != nil && s.OnError != nil {
		s.OnError(result)
	}
	return result
}

func (s *Server) onStop(ctx context.Context) error {
	if s.OnStop == nil {
		return nil
	}
	return protect("stop", func() error { return s.OnStop(ctx) })
}

func (s *Server) begin(parent context.Context) (context.Context, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != StateNew {
		return nil, ErrAlreadyRunning
	}
	if s.done == nil {
		s.done = make(chan struct{})
	}
	ctx, stop := context.WithCancel(parent)
	s.stop = stop
	s.state = StateStarting
	return ctx, nil
}

func (s *Server) shutdownContext() (context.Context, context.CancelFunc) {
	if s.ShutdownTimeout > 0 {
		return context.WithTimeout(context.Background(), s.ShutdownTimeout)
	}
	return context.WithCancel(context.Background())
}

func (s *Server) setState(st State) { s.mu.Lock(); s.state = st; s.mu.Unlock() }

func (s *Server) finish(result error) {
	s.mu.Lock()
	s.stop()
	s.result = result
	s.state = StateStopped
	close(s.done)
	s.mu.Unlock()
}

// Stop starts graceful shutdown without waiting.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == StateNew || s.state == StateStopped {
		return ErrNotRunning
	}
	s.stop()
	return nil
}

// Shutdown starts graceful shutdown and waits for the loop to finish or ctx
// to end.
func (s *Server) Shutdown(ctx context.Context) error {
	if err := s.Stop(); err != nil {
		return err
	}
	select {
	case <-s.done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.result
	case <-ctx.Done():
		return ctx.Err()
	}
}

func protect(name string, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("netx: %s panic: %v", name, r)
		}
	}()
	return fn()
}

// ignoreCanceled drops the context error Shutdown returns after an abort;
// the abort itself is not a failure of the service.
func ignoreCanceled(err error) error {
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
