package udp

import (
	"bytes"
	"errors"
	"net"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/ninepeach/go-netx/buf"
)

func listen(tb testing.TB, network, address string) *net.UDPConn {
	tb.Helper()
	c, err := net.ListenUDP(network, net.UDPAddrFromAddrPort(netip.MustParseAddrPort(address)))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { c.Close() })
	return c
}

func TestPackets(t *testing.T) {
	for _, tc := range []struct{ network, bind, peer string }{
		{"udp4", "127.0.0.1:0", "127.0.0.1"},
		{"udp6", "[::1]:0", "::1"},
		{"udp", "[::]:0", "127.0.0.1"},
	} {
		t.Run(tc.network, func(t *testing.T) {
			server := listen(t, tc.network, tc.bind)
			addr := netip.AddrPortFrom(netip.MustParseAddr(tc.peer), server.LocalAddr().(*net.UDPAddr).AddrPort().Port())
			client, err := net.DialUDP(tc.network, nil, net.UDPAddrFromAddrPort(addr))
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			for _, c := range []*net.UDPConn{client, server} {
				if err := c.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
					t.Fatal(err)
				}
			}
			for _, size := range []int{0, 1500, 1501, 2048, 2049, 3000} {
				data := bytes.Repeat([]byte{42}, size)
				outbound := buf.Copy(data)
				err := WritePacket(client, Packet{Buffer: outbound})
				outbound.Release()
				if err != nil {
					t.Fatal(err)
				}
				max := 65535
				if size == 1500 || size == 1501 {
					max = 1500
				}
				if size == 0 {
					max = 0
				}
				p, err := ReadPacket(server, max)
				if size == 1501 {
					if !errors.Is(err, ErrTruncated) || p != (Packet{}) {
						t.Fatalf("truncation: %v, %v", p, err)
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				func() {
					defer p.Buffer.Release()
					want := normalize(client.LocalAddr().(*net.UDPAddr).AddrPort())
					if p.Addr != want || !bytes.Equal(p.Buffer.Bytes(), data) {
						t.Fatalf("received %d bytes from %v, want %d from %v", p.Buffer.Len(), p.Addr, size, want)
					}
					if size == 2048 && p.Buffer.Cap() != size || size == 2049 && p.Buffer.Cap() != max+1 {
						t.Fatalf("size %d: buffer capacity %d", size, p.Buffer.Cap())
					}
					if p.Addr.Addr().Is4() {
						mapped := netip.AddrFrom16(p.Addr.Addr().As16())
						p.Addr = netip.AddrPortFrom(mapped, p.Addr.Port())
					}
					if err := WritePacket(server, p); err != nil {
						t.Fatal(err)
					}
				}()
				reply, err := ReadPacket(client, size)
				if err != nil {
					t.Fatal(err)
				}
				equal := bytes.Equal(reply.Buffer.Bytes(), data)
				reply.Buffer.Release()
				if !equal || reply.Addr != addr {
					t.Fatalf("reply from %v, expected %v; payload match=%v", reply.Addr, addr, equal)
				}
			}
		})
	}
}

func TestNativeIPv6AndNAT64(t *testing.T) {
	for _, s := range []string{"[fe80::1%eth0]:53", "[64:ff9b::c000:201]:53", "[2001:db8::1]:53"} {
		addr := netip.MustParseAddrPort(s)
		if got := normalize(addr); got != addr {
			t.Errorf("normalize(%v) = %v", addr, got)
		}
	}
}

func TestIPv6OnlyWrite(t *testing.T) {
	c := listen(t, "udp6", "[::1]:0")
	b := buf.Copy([]byte("payload"))
	defer b.Release()
	if err := WritePacket(c, Packet{Buffer: b, Addr: netip.MustParseAddrPort("127.0.0.1:1234")}); err == nil {
		t.Fatal("IPv4 write succeeded on IPv6-only socket")
	}
	if string(b.Bytes()) != "payload" {
		t.Fatal("failed write changed the buffer")
	}
}

func TestReadInterrupted(t *testing.T) {
	for _, closeConn := range []bool{false, true} {
		c := listen(t, "udp4", "127.0.0.1:0")
		result := make(chan error, 1)
		started := make(chan struct{})
		go func() {
			close(started)
			p, err := ReadPacket(c, 1500)
			if p.Buffer != nil {
				p.Buffer.Release()
			}
			result <- err
		}()
		<-started
		want := os.ErrDeadlineExceeded
		if closeConn {
			want = net.ErrClosed
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
		} else if err := c.SetReadDeadline(time.Now()); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-result:
			if !errors.Is(err, want) {
				t.Fatalf("got %v, want %v", err, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("read did not return")
		}
	}
}

func BenchmarkPacket(b *testing.B) {
	server := listen(b, "udp4", "127.0.0.1:0")
	client := listen(b, "udp4", "127.0.0.1:0")
	for _, tc := range []struct {
		name      string
		size, max int
	}{
		{"small/limited", 1200, 1500},
		{"small/right-sized", 1200, 65535},
		{"large/retained", 3000, 65535},
	} {
		b.Run(tc.name, func(b *testing.B) {
			payload := buf.Copy(make([]byte, tc.size))
			defer payload.Release()
			p := Packet{Buffer: payload, Addr: server.LocalAddr().(*net.UDPAddr).AddrPort()}
			b.ReportAllocs()
			b.SetBytes(int64(tc.size))
			for b.Loop() {
				if err := WritePacket(client, p); err != nil {
					b.Fatal(err)
				}
				received, err := ReadPacket(server, tc.max)
				if err != nil {
					b.Fatal(err)
				}
				received.Buffer.Release()
			}
		})
	}
}
