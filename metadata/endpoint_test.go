package metadata_test

import (
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"

	"github.com/ninepeach/go-netx/metadata"
)

func TestParseEndpoint(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		ip       bool
	}{
		{"192.0.2.1:0", "192.0.2.1:0", true},
		{"0.0.0.0:1", "0.0.0.0:1", true},
		{"[2001:0db8::01]:443", "[2001:db8::1]:443", true},
		{"[fe80::1%eth0]:65535", "[fe80::1%eth0]:65535", true},
		{"[fe80::1%Ethernet 2]:53", "[fe80::1%Ethernet 2]:53", true},
		{"[::ffff:192.0.2.1%eth0]:443", "192.0.2.1:443", true},
		{"[64:ff9b::c000:201]:443", "[64:ff9b::c000:201]:443", true},
		{"Example.COM.:00443", "Example.COM.:443", false},
		{"localhost:1", "localhost:1", false},
		{"xn--bcher-kva.example:0", "xn--bcher-kva.example:0", false},
	} {
		t.Run(tc.in, func(t *testing.T) {
			e, err := metadata.ParseEndpoint(tc.in)
			if err != nil || !e.IsValid() || e.IsIP() != tc.ip || e.IsDomain() == tc.ip || e.String() != tc.want {
				t.Fatalf("parse = %#v, formatted %q, %v", e, e.String(), err)
			}
			if e.Addr.Is4In6() {
				t.Fatal("mapped address was retained")
			}
			again, err := metadata.ParseEndpoint(e.String())
			if err != nil || again != e {
				t.Fatalf("round trip = %#v, %v", again, err)
			}
		})
	}
}

func TestInvalidEndpoint(t *testing.T) {
	for _, s := range []string{
		"", ":443", "example.org", "example.org:", "example.org:-1", "example.org:+1", "example.org:65536", "example.org:http", "example.org: 1",
		"fe80::1:443", "[fe80::1%]:443", "[fe80::1%a\n]:443", "[fe80::1%a]b]:443", "[xyz::1]:443", "[example.org]:443",
		"[192.0.2.1]:443", "256.0.0.1:443", "192.000.2.1:443", "192.0.2.1%eth0:1", "https://example.org:443",
		"x..example:1", "-x.example:1", "x-.example:1", "x_example:1", "x.example..:1", "bücher.example:1",
		strings.Repeat("a", 64) + ".example:1", strings.Repeat(strings.Repeat("a", 63)+".", 4) + "a:1",
	} {
		t.Run(s, func(t *testing.T) {
			e, err := metadata.ParseEndpoint(s)
			if !errors.Is(err, metadata.ErrInvalidEndpoint) || e != (metadata.Endpoint{}) {
				t.Fatalf("parse = %#v, %v", e, err)
			}
		})
	}
}

func TestEndpointStates(t *testing.T) {
	a := netip.MustParseAddr("192.0.2.1")
	for _, e := range []metadata.Endpoint{
		{}, {Port: 443}, {Addr: a, Domain: "example.org"}, {Domain: "bad..name"}, {Domain: "192.0.2.1"},
		{Addr: netip.MustParseAddr("fe80::1").WithZone("x]y")},
	} {
		if e.IsValid() || e.IsIP() || e.IsDomain() || e.String() != "" || e.Normalize() != e {
			t.Fatalf("invalid state accepted: %#v", e)
		}
		if _, err := e.AddrPort(); !errors.Is(err, metadata.ErrInvalidEndpoint) {
			t.Fatalf("AddrPort: %v", err)
		}
	}
	e, err := metadata.FromDomain("does-not-resolve.invalid", 443)
	if err != nil || !e.IsDomain() {
		t.Fatalf("unresolved host: %#v, %v", e, err)
	}
	if _, err := e.AddrPort(); !errors.Is(err, metadata.ErrUnresolvedDomain) {
		t.Errorf("AddrPort: %v", err)
	}
	if _, err := e.TCPAddr(); !errors.Is(err, metadata.ErrUnresolvedDomain) {
		t.Errorf("TCPAddr: %v", err)
	}
	if _, err := e.UDPAddr(); !errors.Is(err, metadata.ErrUnresolvedDomain) {
		t.Errorf("UDPAddr: %v", err)
	}
	mapped := metadata.Endpoint{Addr: netip.MustParseAddr("::ffff:192.0.2.1"), Port: 1}
	if got := mapped.Normalize(); got.Addr != a || got.Port != 1 || mapped.String() != "192.0.2.1:1" {
		t.Fatalf("normalization = %#v, %q", got, mapped.String())
	}
}

func TestSocketRoundTrips(t *testing.T) {
	for _, s := range []string{"192.0.2.1:0", "[2001:db8::1]:1", "[fe80::1%eth0]:65535", "[::ffff:192.0.2.1]:443"} {
		t.Run(s, func(t *testing.T) {
			ap := netip.MustParseAddrPort(s)
			e, err := metadata.FromAddrPort(ap)
			if err != nil {
				t.Fatal(err)
			}
			want := netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
			if got, err := e.AddrPort(); err != nil || got != want {
				t.Fatalf("AddrPort = %v, %v", got, err)
			}
			tcp, err := e.TCPAddr()
			if err != nil {
				t.Fatal(err)
			}
			udp, err := e.UDPAddr()
			if err != nil {
				t.Fatal(err)
			}
			fromTCP, err := metadata.FromTCPAddr(tcp)
			if err != nil || fromTCP != e || tcp.Zone != want.Addr().Zone() {
				t.Fatalf("TCP = %#v, %v", fromTCP, err)
			}
			fromUDP, err := metadata.FromUDPAddr(udp)
			if err != nil || fromUDP != e || udp.Zone != want.Addr().Zone() {
				t.Fatalf("UDP = %#v, %v", fromUDP, err)
			}
			for _, a := range []net.Addr{tcp, udp} {
				if got, err := metadata.FromNetAddr(a); err != nil || got != e {
					t.Fatalf("net.Addr = %#v, %v", got, err)
				}
			}
		})
	}
}

func TestInvalidSocketAddresses(t *testing.T) {
	if _, err := metadata.FromAddrPort(netip.AddrPort{}); !errors.Is(err, metadata.ErrInvalidEndpoint) {
		t.Fatalf("AddrPort: %v", err)
	}
	for _, a := range []net.Addr{
		(*net.TCPAddr)(nil), (*net.UDPAddr)(nil), &net.TCPAddr{Port: 1}, &net.UDPAddr{IP: net.IP{1, 2, 3}},
		&net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: -1}, &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 65536},
		&net.TCPAddr{IP: net.IP{192, 0, 2, 1}, Port: 1, Zone: "eth0"},
	} {
		if _, err := metadata.FromNetAddr(a); !errors.Is(err, metadata.ErrInvalidEndpoint) {
			t.Errorf("%#v: %v", a, err)
		}
	}
	for _, a := range []net.Addr{nil, &net.UnixAddr{Name: "192.0.2.1:443", Net: "unix"}, &net.IPAddr{IP: net.IPv4(192, 0, 2, 1)}} {
		if _, err := metadata.FromNetAddr(a); !errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("%#v: %v", a, err)
		}
	}
}

func FuzzParseEndpoint(f *testing.F) {
	for _, s := range []string{"", "127.0.0.1:0", "[fe80::1%eth0]:443", "[::ffff:192.0.2.1]:1", "example.org.:65535", "[::1]:65536", "[fe80::1%Ethernet 2]:53"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		e, err := metadata.ParseEndpoint(s)
		if err != nil {
			if !errors.Is(err, metadata.ErrInvalidEndpoint) || e != (metadata.Endpoint{}) {
				t.Fatalf("invalid parse result %#v, %v", e, err)
			}
			return
		}
		if !e.IsValid() || e.IsIP() == e.IsDomain() || e.Normalize() != e {
			t.Fatalf("invalid endpoint %#v", e)
		}
		got, err := metadata.ParseEndpoint(e.String())
		if err != nil || got != e {
			t.Fatalf("round trip = %#v, %v, want %#v", got, err, e)
		}
	})
}
