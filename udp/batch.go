package udp

import (
	"math"
	"net"
)

// ReadBatch waits for at least one datagram and reads up to len(dst).
// The caller owns all buffers in dst[:n], including when err is non-nil.
// Oversized datagrams are dropped and reported with ErrTruncated. The
// caller must provide empty slots. Portable fallback reads one datagram.
func ReadBatch(c *net.UDPConn, dst []Packet, max int) (int, error) {
	if max < 0 || max == math.MaxInt {
		panic("udp: invalid receive limit")
	}
	for _, p := range dst {
		if p.Buffer != nil {
			panic("udp: occupied batch destination")
		}
	}
	if len(dst) == 0 {
		return 0, nil
	}
	if len(dst) > 1 {
		n, err, handled := readBatch(c, dst, max)
		if handled {
			return n, err
		}
	}
	p, err := ReadPacket(c, max)
	if err != nil {
		return 0, err
	}
	dst[0] = p
	return 1, nil
}

// WriteBatch sends datagrams in order. n is the number accepted; all
// buffers remain owned by the caller even after a partial write.
func WriteBatch(c *net.UDPConn, packets []Packet) (int, error) {
	if len(packets) > 1 {
		n, err, handled := writeBatch(c, packets)
		if handled {
			return n, err
		}
	}
	for i, p := range packets {
		if err := WritePacket(c, p); err != nil {
			return i, err
		}
	}
	return len(packets), nil
}
