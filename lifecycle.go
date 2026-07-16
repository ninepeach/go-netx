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
	ErrServiceRequired = errors.New("netx: service or OnServe is required")
	ErrServiceConflict = errors.New("netx: Service conflicts with OnServe/OnShutdown")
	ErrAlreadyRunning  = errors.New("netx: server is already running or has stopped")
	ErrNotRunning      = errors.New("netx: server is not running")
)

// Service is the lifecycle contract managed by Server. Implementations block
// in Serve until their context is cancelled or Shutdown asks them to stop.
type Service interface {
	Serve(context.Context) error
	Shutdown(context.Context) error
}

// ServiceFuncs adapts functions to Service. It is useful while integrating
// existing components whose Run and Close methods do not yet accept context.
type ServiceFuncs struct {
	ServeFunc    func(context.Context) error
	ShutdownFunc func(context.Context) error
}

func (f ServiceFuncs) Serve(ctx context.Context) error {
	if f.ServeFunc == nil {
		return ErrServiceRequired
	}
	return f.ServeFunc(ctx)
}

func (f ServiceFuncs) Shutdown(ctx context.Context) error {
	if f.ShutdownFunc == nil {
		return nil
	}
	return f.ShutdownFunc(ctx)
}

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
	default:
		return "unknown"
	}
}

// Server manages one Service from startup through graceful shutdown. A Server
// is intentionally one-shot; create a new value to restart a stopped service.
type Server struct {
	Service Service

	OnStart    func(context.Context) error
	OnServe    func(context.Context) error
	OnShutdown func(context.Context) error
	OnStop     func(context.Context) error
	OnError    func(error)

	Signals         []os.Signal
	ShutdownTimeout time.Duration

	mu     sync.Mutex
	state  State
	cancel context.CancelFunc
	done   chan struct{}
	result error
}

// NewServer creates a lifecycle server. Service is optional so existing code
// can set OnServe and OnShutdown after construction.
func NewServer(services ...Service) *Server {
	s := &Server{state: StateNew, done: make(chan struct{}), ShutdownTimeout: 30 * time.Second}
	if len(services) == 1 {
		s.Service = services[0]
	} else if len(services) > 1 {
		s.result = errors.New("netx: expected at most one Service")
	}
	return s
}

func (s *Server) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Loop serves until SIGINT or SIGTERM, then performs graceful shutdown.
func (s *Server) Loop() error {
	signals := s.Signals
	if len(signals) == 0 {
		signals = []os.Signal{syscall.SIGINT, syscall.SIGTERM}
	}
	ctx, stop := signal.NotifyContext(context.Background(), signals...)
	defer stop()
	return s.LoopContext(ctx)
}

// LoopContext serves until ctx is cancelled or the service returns. It does
// not register OS signal handlers, making it suitable for embedding and tests.
func (s *Server) LoopContext(parent context.Context) (result error) {
	ctx, cancel, err := s.begin(parent)
	if err != nil {
		return err
	}
	defer func() { s.finish(result) }()

	serve, shutdown, err := s.resolve()
	if err != nil {
		return err
	}
	if s.OnStart != nil {
		if err := callHook("start", s.OnStart, ctx); err != nil {
			cancel()
			wait := s.shutdownContext()
			defer wait.stop()
			shutdownErr := callHookContext("shutdown", shutdown, wait.ctx)
			var stopErr error
			if s.OnStop != nil {
				stopErr = callHookContext("stop", s.OnStop, wait.ctx)
			}
			return errors.Join(err, shutdownErr, stopErr)
		}
	}
	s.setState(StateRunning)

	serveResult := make(chan error, 1)
	go func() { serveResult <- callHook("serve", serve, ctx) }()

	var serveErr error
	serveReturned := false
	select {
	case serveErr = <-serveResult:
		serveReturned = true
		cancel()
	case <-ctx.Done():
	}

	s.setState(StateStopping)
	wait := s.shutdownContext()
	defer wait.stop()
	shutdownErr := callHookContext("shutdown", shutdown, wait.ctx)
	if !serveReturned {
		select {
		case serveErr = <-serveResult:
		case <-wait.ctx.Done():
			serveErr = wait.ctx.Err()
		}
	}

	var stopErr error
	if s.OnStop != nil {
		stopErr = callHookContext("stop", s.OnStop, wait.ctx)
	}
	result = errors.Join(normalizeLifecycleError(serveErr), normalizeLifecycleError(shutdownErr), stopErr)
	if result != nil && s.OnError != nil {
		s.OnError(result)
	}
	return result
}

func (s *Server) begin(parent context.Context) (context.Context, context.CancelFunc, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != StateNew {
		return nil, nil, ErrAlreadyRunning
	}
	if s.result != nil {
		return nil, nil, s.result
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.state = StateStarting
	return ctx, cancel, nil
}

func (s *Server) resolve() (func(context.Context) error, func(context.Context) error, error) {
	if s.Service != nil {
		if s.OnServe != nil || s.OnShutdown != nil {
			return nil, nil, ErrServiceConflict
		}
		return s.Service.Serve, s.Service.Shutdown, nil
	}
	if s.OnServe == nil {
		return nil, nil, ErrServiceRequired
	}
	shutdown := s.OnShutdown
	if shutdown == nil {
		shutdown = func(context.Context) error { return nil }
	}
	return s.OnServe, shutdown, nil
}

type shutdownWait struct {
	ctx  context.Context
	stop context.CancelFunc
}

func (s *Server) shutdownContext() shutdownWait {
	if s.ShutdownTimeout > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), s.ShutdownTimeout)
		return shutdownWait{ctx: ctx, stop: cancel}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return shutdownWait{ctx: ctx, stop: cancel}
}

func (s *Server) setState(state State) { s.mu.Lock(); s.state = state; s.mu.Unlock() }

func (s *Server) finish(result error) {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.result = result
	s.state = StateStopped
	close(s.done)
	s.mu.Unlock()
}

// Stop requests cancellation without waiting for shutdown to finish.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != StateStarting && s.state != StateRunning && s.state != StateStopping {
		return ErrNotRunning
	}
	s.cancel()
	return nil
}

// Shutdown requests cancellation and waits for Loop or LoopContext to finish.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if s.state != StateStarting && s.state != StateRunning && s.state != StateStopping {
		s.mu.Unlock()
		return ErrNotRunning
	}
	s.cancel()
	done := s.done
	s.mu.Unlock()
	select {
	case <-done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.result
	case <-ctx.Done():
		return ctx.Err()
	}
}

func callHook(name string, hook func(context.Context) error, ctx context.Context) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("netx: %s panic: %v", name, recovered)
		}
	}()
	return hook(ctx)
}

func callHookContext(name string, hook func(context.Context) error, ctx context.Context) error {
	result := make(chan error, 1)
	go func() { result <- callHook(name, hook, ctx) }()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func normalizeLifecycleError(err error) error {
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
