package udp

import (
	"net"
	"net/netip"

	"github.com/ninepeach/go-netx/buf"
)

// BoundPacketConn adapts a connected UDP socket to net.Conn while retaining
// whole-packet and batch operations. The socket filters unrelated sources.
type BoundPacketConn struct {
	*PacketConn
}

// Bind transfers a connected socket on success. The configured peer must
// match peer. On failure the caller retains the socket.
func Bind(c *PacketConn, peer netip.AddrPort) (*BoundPacketConn, error) {
	peer = normalize(peer)
	addr := c.RemoteAddr()
	if addr == nil || !peer.IsValid() || normalize(addr.(*net.UDPAddr).AddrPort()) != peer {
		return nil, &net.AddrError{Err: "UDP socket is not connected to peer", Addr: peer.String()}
	}
	return &BoundPacketConn{PacketConn: c}, nil
}

// Read receives one datagram into p. If it does not fit, the datagram is
// discarded and ErrTruncated is returned.
func (c *BoundPacketConn) Read(p []byte) (int, error) {
	packet, err := c.ReadPacket()
	if err != nil {
		return 0, err
	}
	defer packet.Buffer.Release()
	if packet.Buffer.Len() > len(p) {
		return 0, ErrTruncated
	}
	return copy(p, packet.Buffer.Bytes()), nil
}

// Write sends p as one datagram.
func (c *BoundPacketConn) Write(p []byte) (int, error) {
	b := buf.As(p)
	defer b.Release()
	if err := c.WritePacket(Packet{Buffer: b}); err != nil {
		return 0, err
	}
	return len(p), nil
}
