package tcp

import (
	"context"
	"net"
	"time"

	"github.com/ninepeach/netx/socket"
)

type Dialer struct{ inner net.Dialer }

type DialOptions struct {
	Timeout   time.Duration
	KeepAlive time.Duration
	Socket    socket.Options
}

func NewDialer(opts DialOptions) *Dialer {
	base := net.Dialer{Timeout: opts.Timeout, KeepAlive: opts.KeepAlive}
	base = socket.Dialer(base, opts.Socket)
	return &Dialer{inner: base}
}

func (d *Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return d.inner.DialContext(ctx, network, address)
}
