//go:build stress

package udp_test

import (
	"context"
	"encoding/binary"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ninepeach/go-netx/internal/leak"
	"github.com/ninepeach/go-netx/socket"
	"github.com/ninepeach/go-netx/udp"
)

// A slow handler behind a tiny receive buffer makes the kernel drop
// datagrams. On loopback nothing else loses packets, so every datagram
// sent is either received or counted in KernelDrops.
//
// Checks: UDP-31
func TestStressKernelDropsAccounting(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("SO_RXQ_OVFL is Linux-only")
	}
	c := start(t, func(*udp.Conn, *udp.Packet) { time.Sleep(200 * time.Microsecond) },
		udp.Options{Socket: socket.Options{RecvBuffer: 4096}})
	if !c.Stats().KernelDropsEnabled {
		t.Fatal("drop counting not enabled")
	}
	cl := dial(t, c)
	const sent = 5000
	payload := make([]byte, 512)
	for i := 0; i < sent; i++ {
		if _, err := cl.Write(payload); err != nil {
			t.Fatal(err)
		}
	}
	// The kernel reports drops along with the next queued datagram, so
	// drops after the last queued one only show up once another datagram
	// arrives. Let the backlog drain, then send one probe.
	settle := time.Now().Add(10 * time.Second)
	for prev := uint64(0); ; {
		time.Sleep(50 * time.Millisecond)
		cur := c.Stats().RecvPackets
		if cur == prev || time.Now().After(settle) {
			break
		}
		prev = cur
	}
	if _, err := cl.Write(payload); err != nil {
		t.Fatal(err)
	}
	const total = sent + 1
	waitFor := time.Now().Add(10 * time.Second)
	for {
		s := c.Stats()
		if s.RecvPackets+s.KernelDrops == total {
			if s.KernelDrops == 0 {
				t.Fatal("no drops: the test did not overload the socket")
			}
			t.Logf("received=%d kernelDrops=%d", s.RecvPackets, s.KernelDrops)
			return
		}
		if time.Now().After(waitFor) {
			t.Fatalf("received %d + dropped %d != sent %d", s.RecvPackets, s.KernelDrops, total)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Many senders through several readers: every reply comes back intact to
// the right sender, and nothing leaks afterwards.
//
// Checks: NF-04
func TestStressUDPEchoManySenders(t *testing.T) {
	base := leak.Take()
	opts := udp.Options{Readers: 4}
	if runtime.GOOS == "linux" {
		opts.Socket.ReusePort = true
	}
	c, err := udp.Listen(context.Background(), "udp", "127.0.0.1:0", echo, opts)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- c.Serve(context.Background()) }()

	const senders, each = 16, 2000
	var lost atomic.Int64
	var wg sync.WaitGroup
	for s := 0; s < senders; s++ {
		wg.Add(1)
		go func(s int) {
			defer wg.Done()
			cl, err := net.DialUDP("udp", nil, net.UDPAddrFromAddrPort(c.LocalAddr()))
			if err != nil {
				t.Error(err)
				return
			}
			defer cl.Close()
			out, in := make([]byte, 64), make([]byte, 128)
			for i := 0; i < each; i++ {
				binary.BigEndian.PutUint32(out[0:], uint32(s))
				binary.BigEndian.PutUint32(out[4:], uint32(i))
				cl.Write(out)
				cl.SetReadDeadline(time.Now().Add(time.Second))
				n, err := cl.Read(in)
				if err != nil {
					lost.Add(1) // tolerated: UDP may drop under load
					continue
				}
				if n != len(out) || binary.BigEndian.Uint32(in[0:]) != uint32(s) || binary.BigEndian.Uint32(in[4:]) > uint32(i) {
					t.Errorf("sender %d got a corrupted or foreign reply", s)
					return
				}
			}
		}(s)
	}
	wg.Wait()
	st := c.Stats()
	t.Logf("recv=%d sent=%d lost=%d drops=%d", st.RecvPackets, st.SendPackets, lost.Load(), st.KernelDrops)
	if lost.Load() > senders*each/100 {
		t.Fatalf("lost %d of %d round trips", lost.Load(), senders*each)
	}
	c.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := base.Check(5*time.Second, 2); err != nil {
		t.Fatal(err)
	}
}
