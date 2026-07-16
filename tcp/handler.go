package tcp

import (
	"context"
	"net"
)

type Handler interface {
	ServeTCP(context.Context, net.Conn) error
}

type HandlerFunc func(context.Context, net.Conn) error

func (f HandlerFunc) ServeTCP(ctx context.Context, conn net.Conn) error {
	return f(ctx, conn)
}
