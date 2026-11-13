package transfer

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestRelayHalfClose(t *testing.T) {
	a, peerA := tcpPair(t)
	b, peerB := tcpPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, c := range []*net.TCPConn{peerA, peerB} {
		if err := c.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan error, 1)
	go func() {
		_, err := CopyConnWithOptions(ctx, a, b, CopyConnOptions{HalfCloseTimeout: 20 * time.Millisecond})
		done <- err
	}()
	exchange := func(src, dst *net.TCPConn, s string) {
		t.Helper()
		if _, err := io.WriteString(src, s); err != nil {
			t.Fatal(err)
		}
		if err := src.CloseWrite(); err != nil {
			t.Fatal(err)
		}
		p, err := io.ReadAll(dst)
		if err != nil || string(p) != s {
			t.Fatalf("got %q, %v", p, err)
		}
	}
	exchange(peerA, peerB, "request")
	select {
	case err := <-done:
		t.Fatalf("supported half-close started a timeout: %v", err)
	case <-time.After(40 * time.Millisecond):
	}
	exchange(peerB, peerA, "reply")
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

type noHalfClose struct{ net.Conn }

type unsupportedHalfClose struct{ net.Conn }

func (unsupportedHalfClose) CloseWrite() error { return errors.ErrUnsupported }

func TestCopyConnWithoutHalfCloseKeepsReverseCopy(t *testing.T) {
	for _, tc := range []struct {
		name string
		wrap func(net.Conn) net.Conn
	}{
		{"missing", func(c net.Conn) net.Conn { return noHalfClose{c} }},
		{"unsupported", func(c net.Conn) net.Conn { return unsupportedHalfClose{c} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, peerA := tcpPair(t)
			b, peerB := tcpPair(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			observed := make(chan DirectionResult, 1)
			done := make(chan error, 1)
			go func() {
				_, err := CopyConnWithOptions(ctx, tc.wrap(a), b, CopyConnOptions{
					HalfCloseTimeout: time.Second,
					Observe: func(d Direction, r DirectionResult) {
						if d == BToA {
							observed <- r
						}
					},
				})
				done <- err
			}()
			if err := peerB.CloseWrite(); err != nil {
				t.Fatal(err)
			}
			select {
			case r := <-observed:
				if r.Bytes != 0 || r.CopyError != nil || (r.HalfCloseAttempted && !errors.Is(r.HalfCloseError, errors.ErrUnsupported)) {
					t.Fatalf("direction: %+v", r)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if _, err := io.WriteString(peerA, "late request"); err != nil {
				t.Fatal(err)
			}
			if err := peerA.CloseWrite(); err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(peerB)
			if string(got) != "late request" || err != nil {
				t.Fatalf("got %q, %v", got, err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func BenchmarkTCPCopy(b *testing.B) {
	for _, variant := range []struct {
		name string
		copy func(io.Writer, io.Reader) (int64, error)
	}{
		{"standard", io.Copy},
		{"capability", Copy},
		{"buffered", CopyBuffered},
	} {
		b.Run(variant.name, func(b *testing.B) {
			src, producer := tcpPair(b)
			dst, consumer := tcpPair(b)
			produced := make(chan struct{})
			consumed := make(chan struct{})
			go func() {
				defer close(produced)
				data := make([]byte, 64<<10)
				for {
					if _, err := producer.Write(data); err != nil {
						return
					}
				}
			}()
			go func() {
				defer close(consumed)
				io.Copy(io.Discard, consumer)
			}()
			defer func() {
				producer.Close()
				consumer.Close()
				<-produced
				<-consumed
			}()
			const size = 1 << 20
			b.SetBytes(size)
			b.ReportAllocs()
			limited := io.LimitedReader{R: src}
			for b.Loop() {
				limited.N = size
				if n, err := variant.copy(dst, &limited); n != size || err != nil {
					b.Fatalf("copied %d: %v", n, err)
				}
			}
		})
	}
}

func BenchmarkConcurrentTCPCopy(b *testing.B) {
	const streams = 4
	const chunk = 256 << 10
	start := make(chan struct{})
	jobs := make([]chan struct{}, 0, streams)
	results := make(chan struct{}, streams)
	var workers sync.WaitGroup
	for range streams {
		src, producer := tcpPair(b)
		dst, consumer := tcpPair(b)
		job := make(chan struct{})
		jobs = append(jobs, job)
		workers.Add(3)
		go func() {
			defer workers.Done()
			<-start
			p := make([]byte, 32<<10)
			for {
				if _, err := producer.Write(p); err != nil {
					return
				}
			}
		}()
		go func() {
			defer workers.Done()
			<-start
			io.Copy(io.Discard, consumer)
		}()
		go func() {
			defer workers.Done()
			<-start
			limited := io.LimitedReader{R: src}
			for range job {
				limited.N = chunk
				if n, err := Copy(dst, &limited); n != chunk || err != nil {
					b.Errorf("stream copied %d: %v", n, err)
				}
				results <- struct{}{}
			}
			producer.Close()
			consumer.Close()
		}()
	}
	b.SetBytes(streams * chunk)
	b.ReportAllocs()
	close(start)
	for b.Loop() {
		for _, job := range jobs {
			job <- struct{}{}
		}
		for range streams {
			<-results
		}
	}
	for _, job := range jobs {
		close(job)
	}
	workers.Wait()
}

// Cancellation swept across a clean finish: before, during and after it.
// Whichever wins, the error and the state of the connections agree, and
// the counts are exact on success and never more than was sent.
func TestCopyConnCancelAtCompletion(t *testing.T) {
	const request, reply = "request", "reply!"
	var clean, cancelled int
	for i := 0; i < 400; i++ {
		a, peerA := tcpPair(t)
		b, peerB := tcpPair(t)
		ctx, cancel := context.WithCancel(context.Background())
		type result struct {
			counts Counts
			err    error
		}
		done := make(chan result, 1)
		go func() { c, err := CopyConn(ctx, a, b); done <- result{c, err} }()

		var wg sync.WaitGroup
		send := func(src, dst *net.TCPConn, s string) {
			defer wg.Done()
			src.SetDeadline(time.Now().Add(5 * time.Second))
			dst.SetDeadline(time.Now().Add(5 * time.Second))
			io.WriteString(src, s)
			src.CloseWrite()
			io.ReadAll(dst) // returns once CopyConn has half-closed this direction
		}
		wg.Add(2)
		go send(peerA, peerB, request)
		go send(peerB, peerA, reply)
		time.Sleep(time.Duration(i%40) * 5 * time.Microsecond) // move the cancel across the finish
		cancel()
		wg.Wait()

		r := <-done
		open := a.SetDeadline(time.Time{}) == nil
		if openB := b.SetDeadline(time.Time{}) == nil; openB != open {
			t.Fatalf("run %d: one connection open, one closed", i)
		}
		switch {
		case r.err == nil && open:
			clean++
		case r.err == context.Canceled && !open:
			cancelled++
		default:
			t.Fatalf("run %d: error %v with connections open=%v", i, r.err, open)
		}
		full := Counts{AToB: int64(len(request)), BToA: int64(len(reply))}
		if r.err == nil && r.counts != full || r.counts.AToB > full.AToB || r.counts.BToA > full.BToA {
			t.Fatalf("run %d: counts %+v with error %v", i, r.counts, r.err)
		}
		for _, c := range []*net.TCPConn{a, b, peerA, peerB} {
			c.Close()
		}
	}
	t.Logf("clean %d, cancelled %d", clean, cancelled)
	if clean == 0 || cancelled == 0 {
		t.Skipf("the sweep did not reach both outcomes on this machine")
	}
}

func tcpPair(t testing.TB) (*net.TCPConn, *net.TCPConn) {
	t.Helper()
	l, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	a, err := net.DialTCP("tcp4", nil, l.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	b, err := l.AcceptTCP()
	if err != nil {
		a.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close(); b.Close() })
	return a, b
}

type eofPipeConn struct{ net.Conn }

func (eofPipeConn) Read([]byte) (int, error) { return 0, io.EOF }

func TestCopyConnUnavailableHalfCloseTimesOut(t *testing.T) {
	a, peerA := net.Pipe()
	defer a.Close()
	defer peerA.Close()
	b, peerB := net.Pipe()
	defer b.Close()
	defer peerB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	counts, err := CopyConnWithOptions(ctx, eofPipeConn{a}, b, CopyConnOptions{
		HalfCloseTimeout: 20 * time.Millisecond,
	})
	if !errors.Is(err, ErrHalfCloseTimeout) || counts != (Counts{}) {
		t.Fatalf("copy = (%+v, %v)", counts, err)
	}
	for _, peer := range []net.Conn{peerA, peerB} {
		_ = peer.SetReadDeadline(time.Now().Add(time.Second))
		if _, err := peer.Read(make([]byte, 1)); err != io.EOF {
			t.Fatalf("peer after timeout: %v", err)
		}
	}
}
