package mux_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/ninepeach/go-netx/mux"
)

type testStream struct {
	net.Conn
	id uint64
}

func (s *testStream) StreamID() uint64  { return s.id }
func (s *testStream) Reset(error) error { return s.Close() }

type testSession struct {
	streams chan mux.Stream
	closed  chan struct{}
	once    sync.Once
}

func (s *testSession) AcceptStream(ctx context.Context) (mux.Stream, error) {
	select {
	case stream := <-s.streams:
		return stream, nil
	case <-s.closed:
		return nil, net.ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (s *testSession) Close() error { s.once.Do(func() { close(s.closed) }); return nil }

type testFactory struct{ session mux.ServerSession }

func (f testFactory) AcceptSession(context.Context, net.Conn) (mux.ServerSession, error) {
	return f.session, nil
}

func TestServerHandlesStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	session := &testSession{streams: make(chan mux.Stream, 1), closed: make(chan struct{})}
	handled := make(chan uint64, 1)
	srv := mux.Server{Factory: testFactory{session}, Handler: mux.StreamHandlerFunc(func(_ context.Context, stream mux.Stream) error {
		handled <- stream.StreamID()
		return nil
	})}
	left, right := net.Pipe()
	defer right.Close()
	done := make(chan error, 1)
	go func() { done <- srv.ServeConn(ctx, left) }()
	a, b := net.Pipe()
	defer b.Close()
	session.streams <- &testStream{Conn: a, id: 42}
	select {
	case id := <-handled:
		if id != 42 {
			t.Fatalf("id %d", id)
		}
	case <-time.After(time.Second):
		t.Fatal("stream not handled")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestServerValidation(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	if err := (&mux.Server{}).ServeConn(context.Background(), left); err == nil {
		t.Fatal("expected validation error")
	}
}

var _ = errors.New
