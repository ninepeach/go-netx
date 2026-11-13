package transfer

import (
	"errors"
	"net/netip"

	"github.com/ninepeach/go-netx/bufio"
)

var (
	// ErrInvalidTarget reports a CopyPacket destination that is not a
	// valid address and port.
	ErrInvalidTarget = errors.New("transfer: invalid packet target")
	// ErrInvalidPacket reports a reader that returned no buffer and no
	// error.
	ErrInvalidPacket = errors.New("transfer: packet without a buffer")
)

// CopyPacket reads one datagram from src and sends it to target through
// dst without copying its payload. It returns the datagram's source, also
// when the write fails. The caller chooses the target and owns both
// endpoints.
//
// CopyPacket owns the datagram between the read and its return and
// releases it exactly once, whatever the write reports. A failed read
// leaves nothing to release. dst borrows the datagram: a writer that
// releases or keeps it breaks the contract, and the release here panics.
func CopyPacket(dst bufio.PacketWriter, src bufio.PacketReader, target netip.AddrPort) (netip.AddrPort, error) {
	target = netip.AddrPortFrom(target.Addr().Unmap(), target.Port())
	if !target.IsValid() {
		return netip.AddrPort{}, ErrInvalidTarget
	}
	p, err := src.ReadPacket()
	if err != nil {
		return netip.AddrPort{}, err
	}
	if p.Buffer == nil {
		return p.Addr, ErrInvalidPacket
	}
	defer p.Buffer.Release()
	source := p.Addr
	p.Addr = target
	return source, dst.WritePacket(p)
}

// CopyPackets moves up to len(scratch) datagrams from src to target through
// dst and returns how many dst accepted. When both ends offer batch
// capabilities one read and one write carry them all; otherwise it moves a
// single datagram as CopyPacket does. scratch is working space the caller
// keeps and reuses, so a steady flow allocates nothing here; its contents
// are overwritten and cleared.
//
// Every datagram read is released exactly once, accepted or not. After a
// partial write the rest are dropped, never resent: the caller cannot tell
// whether the datagram that failed was delivered.
func CopyPackets(dst bufio.PacketWriter, src bufio.PacketReader, target netip.AddrPort, scratch []bufio.Packet) (int, error) {
	br, canRead := src.(bufio.PacketBatchReader)
	bw, canWrite := dst.(bufio.PacketBatchWriter)
	if !canRead || !canWrite || len(scratch) < 2 {
		if _, err := CopyPacket(dst, src, target); err != nil {
			return 0, err
		}
		return 1, nil
	}
	target = netip.AddrPortFrom(target.Addr().Unmap(), target.Port())
	if !target.IsValid() {
		return 0, ErrInvalidTarget
	}
	// Scratch must be empty on entry. Clean every slot after a batch read,
	// including when a broken reader reports an invalid count or fills
	// slots outside its reported count.
	for _, p := range scratch {
		if p.Buffer != nil {
			return 0, bufio.ErrInvalidCount
		}
	}
	defer func() {
		for i := range scratch {
			if scratch[i].Buffer != nil {
				scratch[i].Buffer.Release()
			}
			scratch[i] = bufio.Packet{}
		}
	}()
	n, readErr := br.ReadBatch(scratch)
	if n < 0 || n > len(scratch) {
		return 0, bufio.ErrInvalidCount
	}
	batch := scratch[:n]
	for i := range batch {
		if batch[i].Buffer == nil {
			return 0, ErrInvalidPacket
		}
		batch[i].Addr = target
	}
	if n == 0 {
		return 0, readErr
	}
	sent, err := bw.WriteBatch(batch)
	if sent < 0 || sent > n {
		return 0, bufio.ErrInvalidCount
	}
	if err == nil {
		err = readErr // datagrams that arrived with an error go out first
	}
	return sent, err
}
