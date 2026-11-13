package udp

import (
	"net"
	"net/netip"
	"time"
)

// NATPacketConn exposes a connected UDP peer under a caller-selected logical
// address. The kernel filters packets from unrelated sources.
type NATPacketConn struct {
	bound   *BoundPacketConn
	logical netip.AddrPort
}

// NewNATPacketConn transfers a bound socket to an immutable peer translation
// on success. The caller retains ownership on failure.
func NewNATPacketConn(c *BoundPacketConn, logical netip.AddrPort) (*NATPacketConn, error) {
	logical = normalize(logical)
	if !logical.IsValid() {
		return nil, &net.AddrError{Err: "invalid logical UDP peer", Addr: logical.String()}
	}
	return &NATPacketConn{bound: c, logical: logical}, nil
}

func (c *NATPacketConn) ReadPacket() (Packet, error) {
	p, err := c.bound.ReadPacket()
	if err == nil {
		p.Addr = c.logical
	}
	return p, err
}

func (c *NATPacketConn) WaitReadPacket() (Packet, error) {
	p, err := c.bound.WaitReadPacket()
	if err == nil {
		p.Addr = c.logical
	}
	return p, err
}

func (c *NATPacketConn) ReadBatch(dst []Packet) (int, error) {
	n, err := c.bound.ReadBatch(dst)
	for i := range n {
		dst[i].Addr = c.logical
	}
	return n, err
}

func (c *NATPacketConn) WritePacket(p Packet) error {
	if normalize(p.Addr) != c.logical {
		return c.invalidPeer(p.Addr)
	}
	return c.bound.WritePacket(p)
}

func (c *NATPacketConn) WriteBatch(src []Packet) (int, error) {
	for i, p := range src {
		if normalize(p.Addr) != c.logical {
			n, err := c.bound.WriteBatch(src[:i])
			if err != nil {
				return n, err
			}
			return n, c.invalidPeer(p.Addr)
		}
	}
	return c.bound.WriteBatch(src)
}

func (c *NATPacketConn) invalidPeer(p netip.AddrPort) error {
	return &net.AddrError{Err: "destination differs from logical UDP peer", Addr: p.String()}
}

func (c *NATPacketConn) LocalAddr() net.Addr                { return c.bound.LocalAddr() }
func (c *NATPacketConn) RemoteAddr() net.Addr               { return net.UDPAddrFromAddrPort(c.logical) }
func (c *NATPacketConn) SetDeadline(t time.Time) error      { return c.bound.SetDeadline(t) }
func (c *NATPacketConn) SetReadDeadline(t time.Time) error  { return c.bound.SetReadDeadline(t) }
func (c *NATPacketConn) SetWriteDeadline(t time.Time) error { return c.bound.SetWriteDeadline(t) }
func (c *NATPacketConn) Close() error                       { return c.bound.Close() }
