package tcp

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/ninepeach/go-netx/limit"
)

// Checks: TCP-25
func TestIPKeyGrouping(t *testing.T) {
	tcpAddr := func(s string) net.Addr { return net.TCPAddrFromAddrPort(netip.MustParseAddrPort(s)) }
	for _, tc := range []struct {
		addr string
		bits int
		want string
	}{
		{"203.0.113.7:1000", 64, "203.0.113.7"},
		{"[::ffff:203.0.113.7]:1000", 64, "203.0.113.7"}, // mapped: per IPv4 address
		{"[2001:db8:1:2:aaaa:bbbb:cccc:dddd]:1000", 64, "2001:db8:1:2::"},
		{"[2001:db8:1:2:ffff::1]:1000", 64, "2001:db8:1:2::"}, // same /64, same key
		{"[2001:db8:1:2:aaaa::1]:1000", 48, "2001:db8:1::"},
		{"[2001:db8:1:2:aaaa::1]:1000", 128, "2001:db8:1:2:aaaa::1"},
	} {
		got, ok := ipKey(tcpAddr(tc.addr), tc.bits)
		if !ok || got != netip.MustParseAddr(tc.want) {
			t.Errorf("ipKey(%s,/%d)=%v,%v want %s", tc.addr, tc.bits, got, ok, tc.want)
		}
	}
}

// pipeListener hands out in-memory connections whose remote address is not
// a TCP address.
type pipeListener struct {
	net.Listener
	conns chan net.Conn
}

func (l *pipeListener) Accept() (net.Conn, error) {
	c, ok := <-l.conns
	if !ok {
		return nil, net.ErrClosed
	}
	return c, nil
}
func (l *pipeListener) Close() error { return nil }

// Checks: TCP-26
func TestPerIPSkipsNonTCPAddresses(t *testing.T) {
	if _, ok := ipKey(&net.UnixAddr{Name: "x", Net: "unix"}, 64); ok {
		t.Fatal("unix address keyed")
	}
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer inner.Close()
	ln := &pipeListener{Listener: inner, conns: make(chan net.Conn, 4)}
	served := make(chan struct{}, 4)
	s, err := NewServer(ln, func(context.Context, *Conn) { served <- struct{}{} }, Options{MaxConnsPerIP: 1})
	if err != nil {
		t.Fatal(err)
	}
	go s.Serve(context.Background())
	defer s.Close()
	for i := 0; i < 3; i++ { // more than MaxConnsPerIP, none keyed
		a, b := net.Pipe()
		defer b.Close()
		ln.conns <- a
	}
	for i := 0; i < 3; i++ {
		select {
		case <-served:
		case <-time.After(2 * time.Second):
			t.Fatal("pipe connection limited by MaxConnsPerIP")
		}
	}
	if s.Stats().RejectedPerIP != 0 || s.perIP.Keys() != 0 {
		t.Fatalf("%+v keys=%d", s.Stats(), s.perIP.Keys())
	}
}

// When Admit refuses a connection that already took a per-IP slot, the
// slot is returned.
//
// Checks: TCP-24
func TestAdmitRejectionReturnsPerIPSlot(t *testing.T) {
	s, err := Listen(context.Background(), "tcp", "127.0.0.1:0", func(ctx context.Context, _ *Conn) { <-ctx.Done() },
		Options{MaxConnsPerIP: 5, Admit: limit.NewConcurrency(1)})
	if err != nil {
		t.Fatal(err)
	}
	go s.Serve(context.Background())
	defer s.Close()
	c1, _ := net.Dial("tcp", s.Addr().String())
	defer c1.Close()
	c2, _ := net.Dial("tcp", s.Addr().String())
	defer c2.Close()
	deadline := time.Now().Add(2 * time.Second)
	for s.Stats().Rejected != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("%+v", s.Stats())
		}
		time.Sleep(time.Millisecond)
	}
	key := netip.MustParseAddr("127.0.0.1")
	if n := s.perIP.InUse(key); n != 1 {
		t.Fatalf("per-IP slots held: %d, want 1 (the admitted conn)", n)
	}
	s.Close()
	// The slot is released after Active drops (deferred order), so wait on
	// the slot itself.
	deadline = time.Now().Add(2 * time.Second)
	for s.perIP.Keys() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("slot kept after the connection closed")
		}
		time.Sleep(time.Millisecond)
	}
}
