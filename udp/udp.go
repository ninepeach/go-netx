// Package udp reads and writes whole datagrams on standard UDP connections.
package udp

import (
	"errors"
	"io"
	"math"
	"net"
	"net/netip"

	"github.com/ninepeach/go-netx/buf"
	"github.com/ninepeach/go-netx/bufio"
)

// ErrTruncated reports a discarded datagram larger than the receive limit.
var ErrTruncated = errors.New("udp: datagram exceeds receive limit")

// Packet is a datagram and its peer.
type Packet = bufio.Packet

// ReadPacket reads one datagram of at most max bytes. On success the caller
// owns Buffer and must release it once. An invalid max panics.
func ReadPacket(c *net.UDPConn, max int) (Packet, error) {
	if max < 0 || max == math.MaxInt {
		panic("udp: invalid receive limit")
	}
	b := buf.Get(max + 1)
	n, peer, err := c.ReadFromUDPAddrPort(b.FreeBytes())
	if err != nil || n > max {
		b.Release()
		if n > max || oversized(err) {
			return Packet{}, ErrTruncated
		}
		return Packet{}, err
	}

	return finishPacket(b, n, max, peer), nil
}

func finishPacket(b *buf.Buf, n, max int, peer netip.AddrPort) Packet {
	payload := b.Extend(n)
	if n <= 2<<10 && buf.Capacity(n) < buf.Capacity(max+1) {
		small := buf.Copy(payload)
		b.Release()
		b = small
	}
	return Packet{Buffer: b, Addr: normalize(peer)}
}

// WritePacket sends one datagram. Connected sockets use their remote address.
// The caller retains ownership of p.Buffer on every return.
func WritePacket(c *net.UDPConn, p Packet) error {
	payload := p.Buffer.Bytes()
	var n int
	var err error
	if c.RemoteAddr() != nil {
		n, err = c.Write(payload)
	} else {
		n, err = c.WriteToUDPAddrPort(payload, normalize(p.Addr))
	}
	if err == nil && n != len(payload) {
		return io.ErrShortWrite
	}
	return err
}

func normalize(peer netip.AddrPort) netip.AddrPort {
	return netip.AddrPortFrom(peer.Addr().Unmap(), peer.Port())
}
