package udp

import (
	"math"
	"net"
	"sync"
	"sync/atomic"

	"github.com/ninepeach/go-netx/bufio"
)

var (
	_ bufio.PacketReader      = (*PacketConn)(nil)
	_ bufio.PacketWriter      = (*PacketConn)(nil)
	_ bufio.PacketBatchReader = (*PacketConn)(nil)
	_ bufio.PacketBatchWriter = (*PacketConn)(nil)
)

// PacketConn adapts a UDP socket to owned packet I/O. It counts successful
// datagrams, including each accepted packet in a partially successful batch.
type PacketConn struct {
	*net.UDPConn
	max           int
	read, written atomic.Uint64
	once          sync.Once
	closeErr      error
}

// NewPacketConn binds a receive limit to a standard UDP socket.
func NewPacketConn(c *net.UDPConn, max int) *PacketConn {
	if max < 0 || max == math.MaxInt {
		panic("udp: invalid receive limit")
	}
	return &PacketConn{UDPConn: c, max: max}
}

func (c *PacketConn) ReadPacket() (Packet, error) {
	p, err := ReadPacket(c.UDPConn, c.max)
	if err == nil {
		c.read.Add(1)
	}
	return p, err
}
func (c *PacketConn) WritePacket(p Packet) error {
	err := WritePacket(c.UDPConn, p)
	if err == nil {
		c.written.Add(1)
	}
	return err
}
func (c *PacketConn) ReadBatch(dst []Packet) (int, error) {
	n, err := ReadBatch(c.UDPConn, dst, c.max)
	c.read.Add(uint64(n))
	return n, err
}
func (c *PacketConn) WriteBatch(p []Packet) (int, error) {
	n, err := WriteBatch(c.UDPConn, p)
	c.written.Add(uint64(n))
	return n, err
}

// WaitReadPacket waits for readability before receiving on supported
// platforms. Serialize it with reads on the same socket.
func (c *PacketConn) WaitReadPacket() (Packet, error) {
	if err := waitPacket(c.UDPConn); err != nil {
		return Packet{}, err
	}
	return c.ReadPacket()
}

// PacketCounts reports successful packets sent through the packet methods.
func (c *PacketConn) PacketCounts() (read, written uint64) { return c.read.Load(), c.written.Load() }

// Close closes the UDP socket once.
func (c *PacketConn) Close() error {
	c.once.Do(func() { c.closeErr = c.UDPConn.Close() })
	return c.closeErr
}
