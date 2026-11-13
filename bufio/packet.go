package bufio

import (
	"net/netip"

	"github.com/ninepeach/go-netx/buf"
)

// Packet holds a datagram and its peer: the source when received, the
// destination when sent. Copying a Packet does not copy the buffer or
// transfer ownership.
type Packet struct {
	Buffer *buf.Buf
	Addr   netip.AddrPort
}

// PacketReader returns one complete datagram. On success the buffer is
// non-nil and the caller owns it and must release it once. With an error
// it returns the zero Packet.
type PacketReader interface{ ReadPacket() (Packet, error) }

// PacketWriter sends one complete datagram. It borrows the packet for the
// call and must neither consume nor retain its buffer.
type PacketWriter interface{ WritePacket(Packet) error }

// PacketBatchReader fills dst with up to len(dst) datagrams and returns how
// many. The caller owns each returned buffer. It may return datagrams
// together with an error.
type PacketBatchReader interface {
	ReadBatch(dst []Packet) (int, error)
}

// PacketBatchWriter sends packets in order and returns how many the socket
// accepted. It borrows every packet, accepted or not.
type PacketBatchWriter interface {
	WriteBatch(packets []Packet) (int, error)
}
