package transfer

import (
	"errors"
	"net/netip"
	"testing"

	"github.com/ninepeach/go-netx/buf"
	"github.com/ninepeach/go-netx/bufio"
)

type packetSource struct {
	p   bufio.Packet
	err error
}

func (s packetSource) ReadPacket() (bufio.Packet, error) { return s.p, s.err }

type packetSink func(bufio.Packet) error

func (w packetSink) WritePacket(p bufio.Packet) error { return w(p) }

func TestCopyPacket(t *testing.T) {
	source := netip.MustParseAddrPort("192.0.2.1:123")
	target := netip.MustParseAddrPort("[2001:db8::1]:321")
	fail := errors.New("write failed")
	for _, want := range []error{nil, fail} {
		payload := buf.Copy([]byte("data"))
		got, err := CopyPacket(packetSink(func(p bufio.Packet) error {
			if p.Addr != target || p.Buffer != payload || string(p.Buffer.Bytes()) != "data" {
				t.Errorf("sent %q to %v", p.Buffer.Bytes(), p.Addr)
			}
			return want
		}), packetSource{p: bufio.Packet{Buffer: payload, Addr: source}}, target)
		if got != source || err != want {
			t.Errorf("CopyPacket = %v, %v; want %v, %v", got, err, source, want)
		}
		if payload.Cap() != 0 {
			t.Errorf("write returning %v: datagram not released", want)
		}
	}
}

func TestCopyPacketTarget(t *testing.T) {
	mapped := netip.MustParseAddrPort("[::ffff:192.0.2.9]:53")
	payload := buf.Copy([]byte("x"))
	if _, err := CopyPacket(packetSink(func(p bufio.Packet) error {
		if want := netip.MustParseAddrPort("192.0.2.9:53"); p.Addr != want {
			t.Errorf("target = %v, want %v", p.Addr, want)
		}
		return nil
	}), packetSource{p: bufio.Packet{Buffer: payload}}, mapped); err != nil {
		t.Fatal(err)
	}

	read := false
	src := packetSource{err: errors.New("unused")}
	_, err := CopyPacket(packetSink(func(bufio.Packet) error { read = true; return nil }), src, netip.AddrPort{})
	if err != ErrInvalidTarget || read {
		t.Errorf("invalid target: %v, wrote %v", err, read)
	}
}

func TestCopyPacketWithoutBuffer(t *testing.T) {
	source := netip.MustParseAddrPort("192.0.2.1:9")
	got, err := CopyPacket(packetSink(func(bufio.Packet) error {
		t.Error("wrote a packet that has no buffer")
		return nil
	}), packetSource{p: bufio.Packet{Addr: source}}, netip.MustParseAddrPort("192.0.2.2:9"))
	if got != source || err != ErrInvalidPacket {
		t.Errorf("CopyPacket = %v, %v", got, err)
	}
}

// A writer that releases a borrowed datagram is caught by buf's guard.
func TestCopyPacketWriterMustNotRelease(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("no panic after the writer released a borrowed datagram")
		}
	}()
	CopyPacket(packetSink(func(p bufio.Packet) error { p.Buffer.Release(); return nil }),
		packetSource{p: bufio.Packet{Buffer: buf.Copy([]byte("x"))}}, netip.MustParseAddrPort("192.0.2.2:9"))
}

func TestCopyPacketReadError(t *testing.T) {
	fail := errors.New("read failed")
	got, err := CopyPacket(packetSink(func(bufio.Packet) error {
		t.Error("wrote after a failed read")
		return nil
	}), packetSource{err: fail}, netip.MustParseAddrPort("192.0.2.1:1"))
	if got.IsValid() || err != fail {
		t.Errorf("CopyPacket = %v, %v", got, err)
	}
}
