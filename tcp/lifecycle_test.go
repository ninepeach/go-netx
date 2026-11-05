package tcp_test

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ninepeach/go-netx/limit"
	"github.com/ninepeach/go-netx/tcp"
)

// Checks: TCP-09
func TestConcurrentCloseAndShutdown(t *testing.T) {
	s, done := start(t, func(ctx context.Context, c *tcp.Conn) { <-ctx.Done() }, tcp.Options{})
	dial(t, s)
	waitFor(t, func() bool { return s.Stats().Active == 1 })
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				s.Close()
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			s.Shutdown(ctx)
		}(i)
	}
	wg.Wait()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return")
	}
	if st := s.Stats(); st.Active != 0 {
		t.Fatalf("%+v", st)
	}
}

// Checks: TCP-10
func TestServeTwice(t *testing.T) {
	s, _ := start(t, echo, tcp.Options{})
	dial(t, s) // the first Serve is running
	waitFor(t, func() bool { return s.Stats().Accepted == 1 })
	if err := s.Serve(context.Background()); err == nil || err == tcp.ErrClosed {
		t.Fatalf("second Serve returned %v", err)
	}
}

// Checks: TCP-11
func TestNewServerRejectsNil(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if _, err := tcp.NewServer(nil, echo, tcp.Options{}); err == nil {
		t.Fatal("nil listener accepted")
	}
	if _, err := tcp.NewServer(ln, nil, tcp.Options{}); err == nil {
		t.Fatal("nil handler accepted")
	}
}

// gateListener hands out one connection only when released, and its Close
// does not unblock Accept, so a connection can arrive after StopAccept.
type gateListener struct {
	net.Listener
	release   chan struct{}
	accepting chan struct{} // closed when Serve first calls Accept
	conn      net.Conn
	once      sync.Once
	closed    chan struct{}
}

func (l *gateListener) Accept() (net.Conn, error) {
	var c net.Conn
	l.once.Do(func() {
		close(l.accepting)
		<-l.release
		c = l.conn
	})
	if c != nil {
		return c, nil
	}
	<-l.closed
	return nil, net.ErrClosed
}

func (l *gateListener) Close() error {
	select {
	case <-l.closed:
	default:
		close(l.closed)
	}
	return nil
}

// Checks: TCP-15, TCP-19
func TestConnAcceptedDuringStopIsReleased(t *testing.T) {
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer inner.Close()
	server, client := net.Pipe()
	defer client.Close()
	ln := &gateListener{Listener: inner, release: make(chan struct{}), accepting: make(chan struct{}), conn: server, closed: make(chan struct{})}
	admit := limit.NewConcurrency(1)
	var handled atomic.Bool
	s, err := tcp.NewServer(ln, func(context.Context, *tcp.Conn) { handled.Store(true) }, tcp.Options{Admit: admit})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error)
	go func() { done <- s.Serve(context.Background()) }()

	<-ln.accepting // Serve is running and blocked in Accept
	shut := make(chan error)
	go func() { shut <- s.Shutdown(context.Background()) }()
	<-s.Draining()    // StopAccept has run
	close(ln.release) // a connection arrives after it

	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-shut; err != nil {
		t.Fatal(err)
	}
	if handled.Load() {
		t.Fatal("handler ran for a connection accepted after StopAccept")
	}
	if admit.InUse() != 0 {
		t.Fatalf("admission permit leaked: InUse=%d", admit.InUse())
	}
	client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := client.Read(make([]byte, 1)); err != io.EOF && err != io.ErrClosedPipe {
		t.Fatalf("late connection not closed: %v", err)
	}
	if st := s.Stats(); st.Accepted != 0 || st.Active != 0 {
		t.Fatalf("%+v", st)
	}
}

type ctxKey struct{}

// Checks: TCP-21
func TestHandlerSeesServeContextValues(t *testing.T) {
	got := make(chan any, 1)
	s, err := tcp.Listen(context.Background(), "tcp", "127.0.0.1:0", func(ctx context.Context, _ *tcp.Conn) {
		got <- ctx.Value(ctxKey{})
	}, tcp.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	go s.Serve(context.WithValue(context.Background(), ctxKey{}, "gateway-42"))
	dial(t, s)
	if v := <-got; v != "gateway-42" {
		t.Fatalf("handler saw %v", v)
	}
}

// Checks: OBS-06
func TestStatsReadDuringTraffic(t *testing.T) {
	s, _ := start(t, echo, tcp.Options{})
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = s.Stats()
			}
		}
	}()
	for i := 0; i < 20; i++ {
		c := dial(t, s)
		roundTrip(t, c, "x")
		c.Close()
	}
	close(stop)
	wg.Wait()
	waitFor(t, func() bool { return s.Stats().Closed == 20 })
}
