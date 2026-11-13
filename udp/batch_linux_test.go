//go:build linux && (amd64 || arm64)

package udp

import (
	"net/netip"
	"syscall"
	"testing"
)

func FuzzBatchAddress(f *testing.F) {
	f.Add([]byte{192, 0, 2, 1}, uint16(53), true)
	f.Add([]byte{127, 0, 0, 1}, uint16(0), false)
	f.Add(netip.MustParseAddr("64:ff9b::c000:201").AsSlice(), uint16(123), true)
	f.Fuzz(func(t *testing.T, raw []byte, port uint16, ipv6 bool) {
		ip, ok := netip.AddrFromSlice(raw)
		if !ok {
			return
		}
		addr := netip.AddrPortFrom(ip, port)
		var encoded syscall.RawSockaddrAny
		_, err := encodeName(&encoded, addr, ipv6)
		if ip.Unmap().Is6() && !ipv6 {
			if err != syscall.EAFNOSUPPORT {
				t.Fatalf("IPv6 on IPv4: %v", err)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		got, err := decodeName(&encoded)
		if err != nil || got != normalize(addr) {
			t.Fatalf("round trip = %v, %v, want %v", got, err, normalize(addr))
		}
	})
}
