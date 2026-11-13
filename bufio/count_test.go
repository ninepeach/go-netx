package bufio

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"

	"github.com/ninepeach/go-netx/buf"
)

func TestCounterUnwrap(t *testing.T) {
	c, _ := pipe(t)
	inner := NewCounterConn(c)
	outer := NewCounterConn(inner)
	r, rc := UnwrapCountReader(outer)
	w, wc := UnwrapCountWriter(outer)
	if r != io.Reader(c) || w != io.Writer(c) || len(rc) != 2 || len(wc) != 2 {
		t.Fatalf("unwrapped to %T and %T with %d and %d counters", r, w, len(rc), len(wc))
	}
	rc[0].Add(3)
	wc[1].Add(5)
	if read, _ := outer.Counts(); read != 3 {
		t.Errorf("outer read = %d", read)
	}
	if _, written := inner.Counts(); written != 5 {
		t.Errorf("inner written = %d", written)
	}
	if r, rc := UnwrapCountReader(c); r != io.Reader(c) || rc != nil {
		t.Error("a bare connection was unwrapped")
	}
}

// A counter must not cost a batch its vectorised write, and must count it.
func TestCounterVectorised(t *testing.T) {
	c, s := pipe(t)
	counter := NewCounterConn(c)
	var _ VectorisedWriter = counter
	w := NewWriter(counter)
	w.Add(buf.Copy([]byte("aaaa")))
	w.AddShared([]byte("bbbb"))
	w.Add(buf.Copy([]byte("cccc")))
	if n, err := w.Flush(); n != 12 || err != nil {
		t.Fatalf("Flush = %d, %v", n, err)
	}
	vec := [][]byte{[]byte("dd"), []byte("ee")}
	if n, err := WriteVectorised(c, vec); n != 4 || err != nil {
		t.Fatalf("WriteVectorised = %d, %v", n, err)
	}
	if string(vec[0]) != "dd" || string(vec[1]) != "ee" || len(vec) != 2 {
		t.Fatalf("WriteVectorised changed its vector: %q", vec)
	}
	got := make([]byte, 16)
	if _, err := io.ReadFull(s, got); err != nil || !bytes.Equal(got, []byte("aaaabbbbccccddee")) {
		t.Fatalf("peer read %q, %v", got, err)
	}
	if _, written := counter.Counts(); written != 12 {
		t.Fatalf("counted %d bytes written", written)
	}
}

func TestTCPReadWaiterIdleAndEOF(t *testing.T) {
	c, s := pipe(t)
	w, ok := NewReadWaiter(c)
	if !ok {
		t.Skip("no read-wait on this platform")
	}
	s.Write([]byte("hello"))
	b, err := w.WaitReadBuffer(64)
	if err != nil || string(b.Bytes()) != "hello" {
		t.Fatalf("WaitReadBuffer = %v, %v", b, err)
	}
	b.Release()

	c.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	b, err = w.WaitReadBuffer(64)
	if ne, ok := err.(net.Error); b != nil || !ok || !ne.Timeout() {
		t.Fatalf("idle read = %v, %v; want a timeout and no buffer", b, err)
	}
	c.SetReadDeadline(time.Time{})

	s.Write([]byte("abcdef"))
	s.CloseWrite()
	var got []byte
	for {
		b, err := w.WaitReadBuffer(4) // smaller than what is waiting
		if b != nil {
			got = append(got, b.Bytes()...)
			b.Release()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if string(got) != "abcdef" {
		t.Fatalf("read %q before EOF", got)
	}
}

// plain is a connection type that only adds identity: it declares itself
// pass-through, and counts the Write calls that reach it.
type plain struct {
	*net.TCPConn
	writes int
}

func (p *plain) Write(b []byte) (int, error) { p.writes++; return p.TCPConn.Write(b) }
func (p *plain) UnwrapReader(outer io.Reader) io.Reader {
	if outer != p {
		return nil
	}
	return p.TCPConn
}
func (p *plain) UnwrapWriter(outer io.Writer) io.Writer {
	if outer != p {
		return nil
	}
	return p.TCPConn
}

// opaque is the same without the declaration, as TLS would be.
type opaque struct {
	*net.TCPConn
	writes int
}

func (o *opaque) Write(b []byte) (int, error) { o.writes++; return o.TCPConn.Write(b) }

// A pass-through wrapper keeps the socket's vectorised write, alone and
// under a counter; a wrapper that says nothing is never stepped around.
func TestPassThroughWrapper(t *testing.T) {
	c, s := pipe(t)
	p := &plain{TCPConn: c}
	if r, counters := UnwrapCountReader(p); r != io.Reader(c) || counters != nil {
		t.Fatalf("unwrapped a pass-through wrapper to %T with %d counters", r, len(counters))
	}
	counter := NewCounterConn(p)
	if w, counters := UnwrapCountWriter(counter); w != io.Writer(c) || len(counters) != 1 {
		t.Fatalf("unwrapped counter over pass-through to %T with %d counters", w, len(counters))
	}
	flush := func(dst io.Writer) {
		t.Helper()
		w := NewWriter(dst)
		w.AddShared([]byte("ab"))
		w.AddShared([]byte("cd"))
		if n, err := w.Flush(); n != 4 || err != nil {
			t.Fatalf("Flush = %d, %v", n, err)
		}
		got := make([]byte, 4)
		if _, err := io.ReadFull(s, got); err != nil || string(got) != "abcd" {
			t.Fatalf("peer read %q, %v", got, err)
		}
	}
	flush(p)
	flush(counter)
	if p.writes != 0 {
		t.Errorf("pass-through wrapper saw %d Write calls; the batch should have gone out as one vector", p.writes)
	}
	if _, written := counter.Counts(); written != 4 {
		t.Errorf("counter saw %d bytes", written)
	}

	o := &opaque{TCPConn: c}
	if r, _ := UnwrapCountReader(o); r != io.Reader(o) {
		t.Fatal("an undeclared wrapper was unwrapped")
	}
	flush(o)
	if o.writes != 2 {
		t.Errorf("undeclared wrapper saw %d Write calls, want one per segment", o.writes)
	}
}
