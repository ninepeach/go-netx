package udp_test

import (
	"context"
	"net"
	"net/netip"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ninepeach/go-netx/udp"
)

type received struct {
	from netip.AddrPort
	data string
}

// holdingServer starts a Conn whose handler blocks on the first datagram
// until release is called, so a test can queue a burst behind it.
func holdingServer(t *testing.T, o udp.Options) (c *udp.Conn, got func() []received, waitN func(int), release func()) {
	t.Helper()
	var mu sync.Mutex
	var list []received
	var n atomic.Int64
	gate := make(chan struct{})
	c = start(t, func(_ *udp.Conn, p *udp.Packet) {
		if n.Add(1) == 1 {
			<-gate
		}
		mu.Lock()
		list = append(list, received{p.From, string(p.Data)}) // copy: Data is borrowed
		mu.Unlock()
	}, o)
	got = func() []received {
		mu.Lock()
		defer mu.Unlock()
		return append([]received(nil), list...)
	}
	waitN = func(want int) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for int(n.Load()) < want {
			if time.Now().After(deadline) {
				t.Fatalf("handler saw %d of %d datagrams; stats %+v", n.Load(), want, c.Stats())
			}
			time.Sleep(time.Millisecond)
		}
	}
	var once sync.Once
	release = func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	return c, got, waitN, release
}

// Checks: UDP-32
func TestBurstFromTwoSendersDeliveredInOrder(t *testing.T) {
	c, got, waitN, release := holdingServer(t, udp.Options{})
	a, b := dial(t, c), dial(t, c)
	a.Write([]byte("hold"))
	waitN(1)
	const per = 40 // more than one batch from each sender
	for i := 0; i < per; i++ {
		a.Write([]byte{'a', byte(i)})
		b.Write([]byte{'b', byte(i)})
	}
	release()
	waitN(1 + 2*per)

	from := map[byte]netip.AddrPort{
		'a': a.LocalAddr().(*net.UDPAddr).AddrPort(),
		'b': b.LocalAddr().(*net.UDPAddr).AddrPort(),
	}
	next := map[byte]int{}
	for _, r := range got()[1:] {
		if len(r.data) != 2 {
			t.Fatalf("payload %q", r.data)
		}
		s := r.data[0]
		if r.from != from[s] {
			t.Fatalf("datagram %q from %v, want %v", r.data, r.from, from[s])
		}
		if int(r.data[1]) != next[s] {
			t.Fatalf("sender %c: got #%d, want #%d (order or contents wrong)", s, r.data[1], next[s])
		}
		next[s]++
	}
	if next['a'] != per || next['b'] != per {
		t.Fatalf("delivered %v, want %d each", next, per)
	}
	if s := c.Stats(); s.RecvPackets != 1+2*per || s.RecvBytes != 4+4*per {
		t.Fatalf("stats %+v", s)
	}
}

// Checks: UDP-35
func TestTruncatedInsideBurst(t *testing.T) {
	c, got, waitN, release := holdingServer(t, udp.Options{MaxPacket: 100})
	cl := dial(t, c)
	cl.Write([]byte("hold"))
	waitN(1)
	cl.Write([]byte("before"))
	cl.Write(make([]byte, 101)) // one byte over MaxPacket
	cl.Write([]byte("after"))
	release()
	waitN(3)
	deadline := time.Now().Add(2 * time.Second)
	for c.Stats().Truncated != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	r := got()
	if s := c.Stats(); s.Truncated != 1 || s.RecvPackets != 3 {
		t.Fatalf("stats %+v, want 1 truncated and 3 delivered", s)
	}
	if len(r) != 3 || r[1].data != "before" || r[2].data != "after" {
		t.Fatalf("delivered %v", r)
	}
}

// Checks: UDP-33
func TestBatchOptionBoundaries(t *testing.T) {
	listen := func(o udp.Options) error {
		c, err := udp.Listen(context.Background(), "udp", "127.0.0.1:0", echo, o)
		if err == nil {
			c.Close()
		}
		return err
	}
	if err := listen(udp.Options{Batch: -1}); err == nil {
		t.Fatal("negative Batch accepted")
	}
	for _, b := range []int{0, 1} {
		if err := listen(udp.Options{Batch: b}); err != nil {
			t.Fatalf("Batch %d: %v", b, err)
		}
	}
	if runtime.GOOS == "linux" {
		for _, b := range []int{2, 1024} {
			if err := listen(udp.Options{Batch: b}); err != nil {
				t.Fatalf("Batch %d: %v", b, err)
			}
		}
		if err := listen(udp.Options{Batch: 1025}); err == nil || !strings.Contains(err.Error(), "exceeds 1024") {
			t.Fatalf("Batch 1025: %v", err)
		}
		return
	}
	// Without recvmmsg, asking for a batch is an error, not a fallback.
	if err := listen(udp.Options{Batch: 8}); err == nil || !strings.Contains(err.Error(), "recvmmsg") {
		t.Fatalf("Batch 8 on %s: %v", runtime.GOOS, err)
	}
}
