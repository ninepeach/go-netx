package udp

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

const defaultClientPacketSize = 64 * 1024

// Client is a connected UDP client. Exchange calls are serialized so each
// response is paired with the request that immediately preceded it.
type Client struct {
	conn          net.Conn
	maxPacketSize int
	mu            sync.Mutex
}

type ClientOptions struct {
	MaxPacketSize int
}

// Dial creates a connected UDP client. Context cancellation interrupts the
// initial dial; Exchange applies its own context to subsequent I/O.
func Dial(ctx context.Context, network, address string, opts ClientOptions) (*Client, error) {
	if opts.MaxPacketSize < 0 {
		return nil, errors.New("udp: MaxPacketSize must not be negative")
	}
	if opts.MaxPacketSize == 0 {
		opts.MaxPacketSize = defaultClientPacketSize
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	return NewClient(conn, opts)
}

// NewClient wraps an existing connected datagram socket.
func NewClient(conn net.Conn, opts ClientOptions) (*Client, error) {
	if conn == nil {
		return nil, errors.New("udp: nil client connection")
	}
	if opts.MaxPacketSize < 0 {
		return nil, errors.New("udp: MaxPacketSize must not be negative")
	}
	if opts.MaxPacketSize == 0 {
		opts.MaxPacketSize = defaultClientPacketSize
	}
	return &Client{conn: conn, maxPacketSize: opts.MaxPacketSize}, nil
}

func (c *Client) LocalAddr() net.Addr  { return c.conn.LocalAddr() }
func (c *Client) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }
func (c *Client) Close() error         { return c.conn.Close() }

// Exchange writes one datagram and waits for one response datagram.
func (c *Client) Exchange(ctx context.Context, payload []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(payload) > c.maxPacketSize {
		return nil, errors.New("udp: payload exceeds MaxPacketSize")
	}

	if deadline, ok := ctx.Deadline(); ok {
		if err := c.conn.SetDeadline(deadline); err != nil {
			return nil, err
		}
	}
	stop := context.AfterFunc(ctx, func() { _ = c.conn.SetDeadline(time.Now()) })
	defer func() {
		stop()
		_ = c.conn.SetDeadline(time.Time{})
	}()

	if _, err := c.conn.Write(payload); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	buf := make([]byte, c.maxPacketSize)
	n, err := c.conn.Read(buf)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return buf[:n], nil
}
