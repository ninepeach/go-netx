package udp_test

import (
	"testing"
	"time"

	"github.com/ninepeach/go-netx/socket"
	"github.com/ninepeach/go-netx/udp"
)

// Checks: UDP-36
func TestKernelDropsCountedWithBatch(t *testing.T) {
	c, _, waitN, release := holdingServer(t, udp.Options{Batch: 8, Socket: socket.Options{RecvBuffer: 4096}})
	cl := dial(t, c)
	cl.Write([]byte("hold"))
	waitN(1)
	for i := 0; i < 200; i++ { // far more than a 4KB queue holds
		cl.Write(make([]byte, 500))
	}
	release()
	// The kernel reports drops with the next datagram it queues.
	deadline := time.Now().Add(3 * time.Second)
	for c.Stats().KernelDrops == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("no kernel drops counted: %+v", c.Stats())
		}
		cl.Write([]byte("probe"))
		time.Sleep(5 * time.Millisecond)
	}
	s := c.Stats()
	if s.RecvPackets+s.KernelDrops < 201 {
		t.Fatalf("received %d + dropped %d < 201 sent", s.RecvPackets, s.KernelDrops)
	}
}
