package udp

import (
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ninepeach/go-netx/buf"
)

func TestBatchBoundaries(t *testing.T) {
	for _, connected := range []bool{false, true} {
		recv := listen(t, "udp4", "127.0.0.1:0")
		target := recv.LocalAddr().(*net.UDPAddr)
		var send *net.UDPConn
		if connected {
			var err error
			send, err = net.DialUDP("udp4", nil, target)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { send.Close() })
		} else {
			send = listen(t, "udp4", "127.0.0.1:0")
		}
		if err := recv.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		sender, receiver := NewPacketConn(send, 8), NewPacketConn(recv, 8)
		var packets []Packet
		for _, s := range []string{"first", "", strings.Repeat("x", 128), "last"} {
			b := buf.Copy([]byte(s))
			defer b.Release()
			packets = append(packets, Packet{Buffer: b, Addr: target.AddrPort()})
		}
		if n, err := sender.WriteBatch(packets); n != len(packets) || err != nil {
			t.Fatalf("connected=%t: WriteBatch = %d, %v", connected, n, err)
		}
		dst := make([]Packet, 8)
		want := []string{"first", "", "last"}
		truncated := false
		for len(want) > 0 || !truncated {
			n, err := receiver.ReadBatch(dst)
			if errors.Is(err, ErrTruncated) {
				truncated = true
			} else if err != nil {
				t.Fatal(err)
			}
			for i := range n {
				p := dst[i]
				dst[i] = Packet{}
				data := string(p.Buffer.Bytes())
				p.Buffer.Release()
				if len(want) == 0 || data != want[0] || p.Addr != normalize(send.LocalAddr().(*net.UDPAddr).AddrPort()) {
					t.Fatalf("unexpected %q from %v (want %v)", data, p.Addr, want)
				}
				want = want[1:]
			}
		}
		if r, w := receiver.PacketCounts(); r != 3 || w != 0 {
			t.Fatalf("receive counters = %d,%d", r, w)
		}
		if r, w := sender.PacketCounts(); r != 0 || w != 4 {
			t.Fatalf("send counters = %d,%d", r, w)
		}
	}
}

func TestBatchPartialWrite(t *testing.T) {
	recv := listen(t, "udp4", "127.0.0.1:0")
	send := listen(t, "udp4", "127.0.0.1:0")
	target := recv.LocalAddr().(*net.UDPAddr).AddrPort()
	one := buf.Copy([]byte("first"))
	large := buf.Get(65536)
	large.Extend(65536)
	defer one.Release()
	defer large.Release()
	n, err := WriteBatch(send, []Packet{{Buffer: one, Addr: target}, {Buffer: large, Addr: target}, {Buffer: one, Addr: target}})
	if n != 1 || err == nil {
		t.Fatalf("WriteBatch = %d, %v", n, err)
	}
	if err := recv.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	p, err := ReadPacket(recv, 8)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(p.Buffer.Bytes()); got != "first" {
		t.Errorf("received %q", got)
	}
	p.Buffer.Release()
	if err := recv.SetReadDeadline(time.Now().Add(25 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPacket(recv, 8); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("unexpected extra datagram: %v", err)
	}
}

func TestPacketReadWait(t *testing.T) {
	recv := NewPacketConn(listen(t, "udp4", "127.0.0.1:0"), 8)
	send := listen(t, "udp4", "127.0.0.1:0")
	if err := recv.SetReadDeadline(time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := recv.WaitReadPacket(); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("wait = %v", err)
	}
	if err := recv.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := send.WriteToUDP(nil, recv.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}
	p, err := recv.WaitReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if p.Buffer.Len() != 0 {
		t.Error("nonempty packet")
	}
	p.Buffer.Release()
	if err := recv.Close(); err != nil {
		t.Fatal(err)
	}
	if err := recv.Close(); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkUDPBatch(b *testing.B) {
	for _, size := range []int{1, 8, 32} {
		b.Run(map[int]string{1: "1", 8: "8", 32: "32"}[size], func(b *testing.B) {
			recv := listen(b, "udp4", "127.0.0.1:0")
			send, err := net.DialUDP("udp4", nil, recv.LocalAddr().(*net.UDPAddr))
			if err != nil {
				b.Fatal(err)
			}
			defer send.Close()
			payload := buf.Copy(make([]byte, 256))
			defer payload.Release()
			packets := make([]Packet, size)
			for i := range packets {
				packets[i] = Packet{Buffer: payload}
			}
			dst := make([]Packet, size)
			b.SetBytes(int64(size * 256))
			b.ReportAllocs()
			for b.Loop() {
				if n, err := WriteBatch(send, packets); n != size || err != nil {
					b.Fatalf("send %d: %v", n, err)
				}
				for received := 0; received < size; {
					n, err := ReadBatch(recv, dst, 256)
					if err != nil {
						b.Fatal(err)
					}
					for i := range n {
						dst[i].Buffer.Release()
						dst[i] = Packet{}
					}
					received += n
				}
			}
		})
	}
}

func TestBatchConnectedReceive(t *testing.T) {
	for _, family := range []struct{ network, address string }{{"udp4", "127.0.0.1:0"}, {"udp6", "[::1]:0"}} {
		t.Run(family.network, func(t *testing.T) {
			sender := listen(t, family.network, family.address)
			remote := sender.LocalAddr().(*net.UDPAddr)
			recv, err := net.DialUDP(family.network, nil, remote)
			if err != nil {
				t.Fatal(err)
			}
			defer recv.Close()
			if err := recv.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
				t.Fatal(err)
			}
			b := buf.Copy([]byte("packet"))
			defer b.Release()
			p := Packet{Buffer: b, Addr: recv.LocalAddr().(*net.UDPAddr).AddrPort()}
			if n, err := WriteBatch(sender, []Packet{p, p}); n != 2 || err != nil {
				t.Fatalf("send = %d, %v", n, err)
			}
			var dst [2]Packet
			for left := 2; left > 0; {
				n, err := ReadBatch(recv, dst[:left], 6)
				if err != nil {
					t.Fatal(err)
				}
				for i := range n {
					packet := dst[i]
					dst[i] = Packet{}
					data := string(packet.Buffer.Bytes())
					packet.Buffer.Release()
					if data != "packet" || packet.Addr != remote.AddrPort() {
						t.Fatalf("received %q from %v", data, packet.Addr)
					}
				}
				left -= n
			}
		})
	}
}

func TestOffloadProbeDoesNotEnableOptions(t *testing.T) {
	c := listen(t, "udp4", "127.0.0.1:0")
	s, g, err := OffloadSupport(c)
	if err != nil {
		t.Fatal(err)
	}
	s2, g2, err := OffloadSupport(c)
	if err != nil || s != s2 || g != g2 {
		t.Fatalf("probe changed result: %t %t -> %t %t, %v", s, g, s2, g2, err)
	}
}
