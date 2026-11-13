package udp

import (
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/ninepeach/go-netx/buf"
)

var _ net.Conn = (*BoundPacketConn)(nil)

func TestBoundPacketConn(t *testing.T) {
	peer := listen(t, "udp4", "127.0.0.1:0")
	foreign := listen(t, "udp4", "127.0.0.1:0")
	peerAddr := peer.LocalAddr().(*net.UDPAddr)
	sock, err := net.DialUDP("udp4", nil, peerAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer sock.Close()
	packet := NewPacketConn(sock, 64)
	if _, err := Bind(packet, foreign.LocalAddr().(*net.UDPAddr).AddrPort()); err == nil {
		t.Fatal("bound wrong peer")
	}
	c, err := Bind(packet, peerAddr.AddrPort())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := peer.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	addr := c.LocalAddr().(*net.UDPAddr)
	if _, err := foreign.WriteToUDP([]byte("bad"), addr); err != nil {
		t.Fatal(err)
	}
	if _, err := peer.WriteToUDP([]byte("good"), addr); err != nil {
		t.Fatal(err)
	}
	got, err := c.WaitReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Buffer.Bytes()) != "good" || got.Addr != peerAddr.AddrPort() {
		t.Errorf("received %q from %v", got.Buffer.Bytes(), got.Addr)
	}
	got.Buffer.Release()
	if _, err := peer.WriteToUDP([]byte("too big"), addr); err != nil {
		t.Fatal(err)
	}
	if n, err := c.Read(make([]byte, 2)); n != 0 || !errors.Is(err, ErrTruncated) {
		t.Fatalf("read = %d, %v", n, err)
	}
	if n, err := c.Write([]byte("reply")); n != 5 || err != nil {
		t.Fatalf("write = %d, %v", n, err)
	}
	reply, err := ReadPacket(peer, 64)
	if err != nil {
		t.Fatal(err)
	}
	if string(reply.Buffer.Bytes()) != "reply" {
		t.Errorf("reply = %q", reply.Buffer.Bytes())
	}
	reply.Buffer.Release()
}

func TestFallbackPacketConn(t *testing.T) {
	recv := listen(t, "udp4", "127.0.0.1:0")
	send := listen(t, "udp4", "127.0.0.1:0")
	r, w := NewFallbackPacketConn(NewPacketConn(recv, 16)), NewFallbackPacketConn(NewPacketConn(send, 16))
	defer r.Close()
	defer w.Close()
	if err := r.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if n, err := w.WriteTo([]byte("abc"), r.LocalAddr()); n != 3 || err != nil {
		t.Fatalf("write = %d, %v", n, err)
	}
	var p [4]byte
	if n, addr, err := r.ReadFrom(p[:]); n != 3 || addr.String() != w.LocalAddr().String() || err != nil || string(p[:n]) != "abc" {
		t.Fatalf("read = %d, %v, %v, %q", n, addr, err, p)
	}
	if _, err := w.WriteTo([]byte("long"), r.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	if n, addr, err := r.ReadFrom(p[:1]); n != 0 || addr.String() != w.LocalAddr().String() || err != ErrTruncated {
		t.Fatalf("truncation = %d, %v, %v", n, addr, err)
	}
}

func TestNATBatchAndReadWait(t *testing.T) {
	peer := listen(t, "udp4", "127.0.0.1:0")
	addr := peer.LocalAddr().(*net.UDPAddr)
	s, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	bound, err := Bind(NewPacketConn(s, 64), addr.AddrPort())
	if err != nil {
		t.Fatal(err)
	}
	logical := netip.MustParseAddrPort("[2001:db8::1]:81")
	c, err := NewNATPacketConn(bound, logical)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	deadline := time.Now().Add(3 * time.Second)
	if err := c.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	if err := peer.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	b := buf.Copy([]byte("out"))
	defer b.Release()
	if n, err := c.WriteBatch([]Packet{{Buffer: b, Addr: logical}, {Buffer: b, Addr: addr.AddrPort()}}); n != 1 || err == nil {
		t.Fatalf("write = %d, %v", n, err)
	}
	p, err := ReadPacket(peer, 64)
	if err != nil {
		t.Fatal(err)
	}
	if string(p.Buffer.Bytes()) != "out" {
		t.Errorf("outbound = %q", p.Buffer.Bytes())
	}
	p.Buffer.Release()
	for range 2 {
		if _, err := peer.WriteToUDPAddrPort([]byte("in"), p.Addr); err != nil {
			t.Fatal(err)
		}
	}
	r, err := c.WaitReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if r.Addr != logical || string(r.Buffer.Bytes()) != "in" {
		t.Errorf("receive %v %q", r.Addr, r.Buffer.Bytes())
	}
	r.Buffer.Release()
	var batch [2]Packet
	if n, err := c.ReadBatch(batch[:]); n != 1 || err != nil {
		t.Fatalf("read = %d, %v", n, err)
	}
	if batch[0].Addr != logical {
		t.Errorf("batch peer = %v", batch[0].Addr)
	}
	batch[0].Buffer.Release()
}

func BenchmarkNATPacketRoundTrip(b *testing.B) {
	peer := listen(b, "udp4", "127.0.0.1:0")
	addr := peer.LocalAddr().(*net.UDPAddr)
	sock, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		b.Fatal(err)
	}
	defer sock.Close()
	bound, err := Bind(NewPacketConn(sock, 256), addr.AddrPort())
	if err != nil {
		b.Fatal(err)
	}
	logical := netip.MustParseAddrPort("192.0.2.1:5555")
	c, err := NewNATPacketConn(bound, logical)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	payload := buf.Copy(make([]byte, 256))
	defer payload.Release()
	b.SetBytes(256)
	b.ReportAllocs()
	for b.Loop() {
		if err := c.WritePacket(Packet{Buffer: payload, Addr: logical}); err != nil {
			b.Fatal(err)
		}
		p, err := ReadPacket(peer, 256)
		if err != nil {
			b.Fatal(err)
		}
		err = WritePacket(peer, p)
		p.Buffer.Release()
		if err != nil {
			b.Fatal(err)
		}
		reply, err := c.ReadPacket()
		if err != nil {
			b.Fatal(err)
		}
		reply.Buffer.Release()
	}
}
