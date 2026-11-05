package tcp

import (
	"context"
	"net"
	"time"

	"github.com/ninepeach/go-netx/socket"
)

type DialEvent struct {
	Network    string
	Address    string
	StartedAt  time.Time
	Duration   time.Duration
	LocalAddr  net.Addr
	RemoteAddr net.Addr
	Err        error
}

// Dialer dials TCP with socket options and an optional per-attempt event.
// netx does not aggregate, score, log or retry targets.
type Dialer struct {
	inner  net.Dialer
	onDial func(DialEvent)
}

type DialOptions struct {
	Timeout   time.Duration
	KeepAlive time.Duration
	Socket    socket.Options
	OnDial    func(DialEvent)
}

func NewDialer(opts DialOptions) *Dialer {
	base := net.Dialer{Timeout: opts.Timeout, KeepAlive: opts.KeepAlive}
	base = socket.Dialer(base, opts.Socket)
	return &Dialer{inner: base, onDial: opts.OnDial}
}

func (d *Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	startedAt := time.Now()
	conn, err := d.inner.DialContext(ctx, network, address)
	if d.onDial != nil {
		event := DialEvent{
			Network: network, Address: address, StartedAt: startedAt,
			Duration: time.Since(startedAt), Err: err,
		}
		if conn != nil {
			event.LocalAddr = conn.LocalAddr()
			event.RemoteAddr = conn.RemoteAddr()
		}
		d.onDial(event)
	}
	return conn, err
}
