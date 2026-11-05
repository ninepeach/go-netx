//go:build stress

package tcp_test

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ninepeach/go-netx/internal/leak"
	"github.com/ninepeach/go-netx/limit"
	"github.com/ninepeach/go-netx/tcp"
)

// Thousands of connections open and close concurrently through admission
// control; afterwards every goroutine and file descriptor is returned.
//
// Checks: NF-03, TCP-22
func TestStressTCPChurn(t *testing.T) {
	base := leak.Take()
	admit := limit.NewConcurrency(64)
	s, err := tcp.Listen(context.Background(), "tcp", "127.0.0.1:0", func(_ context.Context, c *tcp.Conn) {
		_, _ = io.Copy(c, c)
	}, tcp.Options{Admit: admit})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Serve(context.Background()) }()

	const workers, perWorker = 100, 30
	var dials, echoed atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 4)
			for i := 0; i < perWorker; i++ {
				c, err := net.Dial("tcp", s.Addr().String())
				if err != nil {
					t.Error(err)
					return
				}
				dials.Add(1)
				c.SetDeadline(time.Now().Add(5 * time.Second))
				if _, err := c.Write([]byte("ping")); err == nil {
					if _, err := io.ReadFull(c, buf); err == nil && string(buf) == "ping" {
						echoed.Add(1)
					}
				}
				c.Close()
			}
		}()
	}
	wg.Wait()

	waitFor(t, func() bool {
		st := s.Stats()
		return st.Active == 0 && st.Accepted+st.Rejected == uint64(dials.Load())
	})
	st := s.Stats()
	if uint64(echoed.Load()) != st.Accepted || st.Closed != st.Accepted {
		t.Fatalf("echoed=%d stats=%+v", echoed.Load(), st)
	}
	if admit.InUse() != 0 {
		t.Fatalf("admission leaked %d permits", admit.InUse())
	}
	t.Logf("dials=%d accepted=%d rejected=%d", dials.Load(), st.Accepted, st.Rejected)

	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := base.Check(5*time.Second, 2); err != nil {
		t.Fatal(err)
	}
}
