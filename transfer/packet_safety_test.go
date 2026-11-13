package transfer

import (
	"errors"
	"net/netip"
	"testing"

	"github.com/ninepeach/go-netx/buf"
	"github.com/ninepeach/go-netx/bufio"
)

type invalidBatchSource struct{ allocated []*buf.Buf }

func (s *invalidBatchSource) ReadPacket() (bufio.Packet, error) {
	return bufio.Packet{}, errors.New("unexpected fallback")
}
func (s *invalidBatchSource) ReadBatch(p []bufio.Packet) (int, error) {
	for i := range p {
		b := buf.Copy([]byte("x"))
		s.allocated = append(s.allocated, b)
		p[i].Buffer = b
	}
	return len(p) + 1, nil
}

type discardBatchSink struct{}

func (discardBatchSink) WritePacket(bufio.Packet) error           { return nil }
func (discardBatchSink) WriteBatch(p []bufio.Packet) (int, error) { return len(p), nil }

func TestCopyPacketsInvalidBatchCountReleasesAll(t *testing.T) {
	src := new(invalidBatchSource)
	scratch := make([]bufio.Packet, 3)
	n, err := CopyPackets(discardBatchSink{}, src, netip.MustParseAddrPort("192.0.2.1:53"), scratch)
	if n != 0 || !errors.Is(err, bufio.ErrInvalidCount) {
		t.Fatalf("got %d, %v", n, err)
	}
	for i, p := range scratch {
		if p.Buffer != nil {
			t.Fatalf("slot %d retained buffer", i)
		}
	}
	for i, b := range src.allocated {
		if b.Cap() != 0 {
			t.Fatalf("buffer %d not released", i)
		}
	}
}
