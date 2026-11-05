package udp_test

import (
	"context"
	"net/netip"
	"testing"

	"github.com/ninepeach/go-netx/udp"
)

// Reply and WriteTo take caller-built values, so arbitrary Reader indexes,
// ports and payload sizes must never panic and must only fail with the
// documented error. Destinations stay on loopback: fuzzing must not send
// datagrams to arbitrary hosts.
//
// Checks: NF-08
func FuzzReplyAndWriteTo(f *testing.F) {
	c, err := udp.Listen(context.Background(), "udp", "127.0.0.1:0", func(*udp.Conn, *udp.Packet) {}, udp.Options{Readers: 2})
	if err != nil {
		f.Fatal(err)
	}
	go c.Serve(context.Background())
	f.Cleanup(func() { c.Close() })
	self := c.LocalAddr()

	f.Add(0, self.Port(), 10, false)
	f.Add(-1, uint16(0), 0, true)
	f.Add(1<<30, uint16(65535), udp.MaxDatagram+1, false)
	f.Add(1, self.Port(), udp.MaxDatagram, false)
	f.Fuzz(func(t *testing.T, reader int, port uint16, size int, nilPacket bool) {
		if size < 0 || size > 70000 {
			size %= 70000
			if size < 0 {
				size = -size
			}
		}
		payload := make([]byte, size)
		to := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), port)
		var p *udp.Packet
		if !nilPacket {
			p = &udp.Packet{From: to, Reader: reader}
		}
		check := func(op string, err error) {
			switch {
			case err == nil, err == udp.ErrTooLarge && size > udp.MaxDatagram,
				err == udp.ErrNilPacket && nilPacket:
			case err == udp.ErrTooLarge || err == udp.ErrNilPacket:
				t.Fatalf("%s: unexpected %v (size %d, nil %v)", op, err, size, nilPacket)
			default:
				// The kernel may refuse some destinations (port 0); that is
				// an ordinary error, not a panic.
			}
			if size > udp.MaxDatagram && err != udp.ErrTooLarge && !(nilPacket && op == "Reply") {
				t.Fatalf("%s: %d-byte payload not refused: %v", op, size, err)
			}
		}
		check("Reply", c.Reply(p, payload))
		check("WriteTo", c.WriteTo(payload, to))
	})
}
