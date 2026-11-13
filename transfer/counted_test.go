package transfer

import (
	"bytes"
	"errors"
	"io"
	"net/netip"
	"testing"
	"time"

	"github.com/ninepeach/go-netx/buf"
	"github.com/ninepeach/go-netx/bufio"
)

// Counters on both ends of a TCP relay: every byte arrives once and is
// counted once on each side. On Linux this is the splice path.
func TestCopyCounted(t *testing.T) {
	a, peerA := tcpPair(t)
	b, peerB := tcpPair(t)
	src, dst := bufio.NewCounterConn(a), bufio.NewCounterConn(b)
	payload := bytes.Repeat([]byte("go-netx!"), 40<<10) // 320KiB, several chunks
	deadline := time.Now().Add(10 * time.Second)
	peerA.SetDeadline(deadline)
	peerB.SetDeadline(deadline)
	go func() {
		peerA.Write(payload)
		peerA.CloseWrite()
	}()
	type result struct {
		n   int64
		err error
	}
	done := make(chan result, 1)
	go func() { n, err := Copy(dst, src); done <- result{n, err} }()
	got, err := io.ReadAll(io.LimitReader(peerB, int64(len(payload))))
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("peer read %d bytes, %v", len(got), err)
	}
	r := <-done
	read, _ := src.Counts()
	_, written := dst.Counts()
	if r.err != nil || r.n != int64(len(payload)) || read != r.n || written != r.n {
		t.Fatalf("Copy = %d, %v; counted %d read, %d written; want %d", r.n, r.err, read, written, len(payload))
	}
}

type batchEnd struct {
	in       []bufio.Packet
	out      []bufio.Packet
	accept   int
	writeErr error
}

func (e *batchEnd) ReadPacket() (bufio.Packet, error) { return bufio.Packet{}, errors.New("unused") }
func (e *batchEnd) WritePacket(bufio.Packet) error    { return errors.New("unused") }
func (e *batchEnd) ReadBatch(dst []bufio.Packet) (int, error) {
	return copy(dst, e.in), nil
}
func (e *batchEnd) WriteBatch(p []bufio.Packet) (int, error) {
	e.out = append(e.out[:0], p...)
	return min(e.accept, len(p)), e.writeErr
}

func TestCopyPackets(t *testing.T) {
	target := netip.MustParseAddrPort("192.0.2.7:53")
	fail := errors.New("socket full")
	for _, c := range []struct {
		accept int
		err    error
	}{{3, nil}, {1, fail}} {
		bufs := []*buf.Buf{buf.Copy([]byte("a")), buf.Copy([]byte("b")), buf.Copy([]byte("c"))}
		src := &batchEnd{}
		for _, b := range bufs {
			src.in = append(src.in, bufio.Packet{Buffer: b, Addr: netip.MustParseAddrPort("198.51.100.1:9")})
		}
		dst := &batchEnd{accept: c.accept, writeErr: c.err}
		scratch := make([]bufio.Packet, 8)
		n, err := CopyPackets(dst, src, target, scratch)
		if n != c.accept || err != c.err || len(dst.out) != 3 {
			t.Fatalf("CopyPackets = %d, %v with %d offered", n, err, len(dst.out))
		}
		for i, p := range dst.out {
			if p.Addr != target || p.Buffer != bufs[i] {
				t.Errorf("packet %d went to %v", i, p.Addr)
			}
		}
		for i, b := range bufs {
			if b.Cap() != 0 {
				t.Errorf("datagram %d not released", i)
			}
		}
		for i, p := range scratch {
			if p.Buffer != nil {
				t.Errorf("scratch[%d] still references a buffer", i)
			}
		}
	}
}
