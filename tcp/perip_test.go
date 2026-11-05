package tcp_test

import (
	"context"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/ninepeach/go-netx/tcp"
)

func dialFrom(t *testing.T, s *tcp.Server, local string) net.Conn {
	t.Helper()
	d := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(local)}}
	c, err := d.Dial("tcp", s.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	c.SetDeadline(time.Now().Add(2 * time.Second))
	return c
}

// Checks: TCP-23
func TestMaxConnsPerIP(t *testing.T) {
	s, _ := start(t, echo, tcp.Options{MaxConnsPerIP: 2})
	a1, a2 := dialFrom(t, s, "127.0.0.1"), dialFrom(t, s, "127.0.0.1")
	roundTrip(t, a1, "a1")
	roundTrip(t, a2, "a2")
	a3 := dialFrom(t, s, "127.0.0.1")
	if _, err := a3.Read(make([]byte, 1)); err == nil {
		t.Fatal("third connection from one IP admitted")
	}
	if runtime.GOOS == "linux" { // the whole 127/8 is local only on Linux
		b := dialFrom(t, s, "127.0.0.2")
		roundTrip(t, b, "other ip")
	}
	st := s.Stats()
	if st.RejectedPerIP != 1 || st.Rejected != 0 {
		t.Fatalf("%+v", st)
	}
}

// Checks: TCP-24
func TestPerIPSlotReleasedOnClose(t *testing.T) {
	s, _ := start(t, echo, tcp.Options{MaxConnsPerIP: 1})
	for i := 0; i < 5; i++ { // sequential connections from one IP all succeed
		c := dialFrom(t, s, "127.0.0.1")
		roundTrip(t, c, "x")
		c.Close()
		want := uint64(i + 1)
		waitFor(t, func() bool { return s.Stats().Closed == want })
	}
	if st := s.Stats(); st.RejectedPerIP != 0 {
		t.Fatalf("slot not released: %+v", st)
	}
}

// Checks: TCP-27
func TestPerIPOptionValidation(t *testing.T) {
	for _, o := range []tcp.Options{
		{MaxConnsPerIP: -1},
		{MaxConnsPerIP: 1, IPv6PrefixBits: -1},
		{MaxConnsPerIP: 1, IPv6PrefixBits: 129},
	} {
		if s, err := tcp.Listen(context.Background(), "tcp", "127.0.0.1:0", echo, o); err == nil {
			s.Close()
			t.Fatalf("%+v accepted", o)
		}
	}
	for _, bits := range []int{0, 48, 128} {
		s, err := tcp.Listen(context.Background(), "tcp", "127.0.0.1:0", echo, tcp.Options{MaxConnsPerIP: 1, IPv6PrefixBits: bits})
		if err != nil {
			t.Fatalf("bits %d: %v", bits, err)
		}
		s.Close()
	}
}
