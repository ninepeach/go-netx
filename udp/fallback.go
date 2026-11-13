package udp

import (
	"net"
	"time"

	"github.com/ninepeach/go-netx/buf"
	"github.com/ninepeach/go-netx/bufio"
)

type packetEndpoint interface {
	bufio.PacketReader
	bufio.PacketWriter
	Close() error
	LocalAddr() net.Addr
	SetDeadline(time.Time) error
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
}

type fallbackPacketConn struct{ packetEndpoint }

// NewFallbackPacketConn exposes a packet endpoint through net.PacketConn.
// Its ReadFrom consumes one datagram, returning ErrTruncated if it does not
// fit. Addresses supplied to WriteTo must be *net.UDPAddr.
func NewFallbackPacketConn(c packetEndpoint) net.PacketConn {
	return &fallbackPacketConn{c}
}

func (c *fallbackPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	packet, err := c.ReadPacket()
	if err != nil {
		return 0, nil, err
	}
	defer packet.Buffer.Release()
	addr := net.UDPAddrFromAddrPort(packet.Addr)
	if packet.Buffer.Len() > len(p) {
		return 0, addr, ErrTruncated
	}
	return copy(p, packet.Buffer.Bytes()), addr, nil
}

func (c *fallbackPacketConn) WriteTo(p []byte, dst net.Addr) (int, error) {
	addr, ok := dst.(*net.UDPAddr)
	if !ok || addr == nil || !addr.AddrPort().IsValid() {
		return 0, &net.AddrError{Err: "invalid UDP destination"}
	}
	target := normalize(addr.AddrPort())
	if conn, ok := c.packetEndpoint.(interface{ RemoteAddr() net.Addr }); ok {
		if peer := conn.RemoteAddr(); peer != nil {
			udpPeer, ok := peer.(*net.UDPAddr)
			if !ok || normalize(udpPeer.AddrPort()) != target {
				return 0, &net.AddrError{Err: "destination differs from connected peer", Addr: target.String()}
			}
		}
	}
	b := buf.Wrap(p)
	defer b.Release()
	if err := c.WritePacket(Packet{Buffer: b, Addr: target}); err != nil {
		return 0, err
	}
	return len(p), nil
}
