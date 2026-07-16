package mux

import (
	"context"
	"net"
)

// Stream is one logical, bidirectional byte stream in a multiplexed session.
type Stream interface {
	net.Conn
	StreamID() uint64
	Reset(error) error
}

type ClientSession interface {
	OpenStream(context.Context) (Stream, error)
	Close() error
}

type ServerSession interface {
	AcceptStream(context.Context) (Stream, error)
	Close() error
}

type ClientFactory interface {
	OpenSession(context.Context) (ClientSession, error)
}

type ServerFactory interface {
	AcceptSession(context.Context, net.Conn) (ServerSession, error)
}

type StreamHandler interface {
	ServeStream(context.Context, Stream) error
}
type StreamHandlerFunc func(context.Context, Stream) error

func (f StreamHandlerFunc) ServeStream(ctx context.Context, stream Stream) error {
	return f(ctx, stream)
}
