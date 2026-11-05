package udp_test

import (
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ninepeach/go-netx/socket"
	"github.com/ninepeach/go-netx/udp"
)

// benchRecv measures the receive path alone: the handler holds the reader
// while the client fills the socket queue, then the timer runs while the
// reader drains the queued datagrams through the handler.
func benchRecv(b *testing.B, o udp.Options, size int) {
	const chunk = 256
	var count atomic.Int64
	gate := make(chan struct{})
	c := start(b, func(_ *udp.Conn, p *udp.Packet) {
		if count.Add(1)%chunk == 1 {
			<-gate
		}
	}, o)
	cl := dial(b, c)
	cl.SetDeadline(time.Time{})
	msg := make([]byte, size)
	wait := func(n int64) {
		deadline := time.Now().Add(5 * time.Second)
		for count.Load() < n {
			if time.Now().After(deadline) {
				b.Fatalf("received %d of %d (kernel drops %d)", count.Load(), n, c.Stats().KernelDrops)
			}
			runtime.Gosched()
		}
	}
	b.ReportAllocs()
	b.SetBytes(int64(size))
	b.ResetTimer()
	b.StopTimer()
	for done := int64(0); done < int64(b.N); done += chunk {
		cl.Write(msg)
		wait(done + 1) // the reader is now held in the handler
		for i := 1; i < chunk; i++ {
			cl.Write(msg)
		}
		b.StartTimer()
		gate <- struct{}{}
		wait(done + chunk)
		b.StopTimer()
	}
}

// Checks: NF-22
func BenchmarkRecv(b *testing.B) {
	sock := socket.Options{RecvBuffer: 2 << 20}
	for _, size := range []int{64, 1200} {
		for _, batch := range []int{1, 8, 32} {
			if batch > 1 && runtime.GOOS != "linux" {
				continue
			}
			b.Run("batch="+strconv.Itoa(batch)+"/"+strconv.Itoa(size)+"B", func(b *testing.B) {
				benchRecv(b, udp.Options{Batch: batch, MaxPacket: 2048, Socket: sock}, size)
			})
		}
	}
}
