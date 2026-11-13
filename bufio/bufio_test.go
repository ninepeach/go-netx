package bufio

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"

	"github.com/ninepeach/go-netx/buf"
)

// step is one result of a scripted Read: data, then err.
type step struct {
	data string
	err  error
}

// script is a source that returns one step per Read and counts its calls.
// When the steps run out it returns (0, nil).
type script struct {
	steps []step
	calls int
}

func (s *script) Read(p []byte) (int, error) {
	s.calls++
	if len(s.steps) == 0 {
		return 0, nil
	}
	st := &s.steps[0]
	n := copy(p, st.data)
	if st.data = st.data[n:]; st.data != "" {
		return n, nil
	}
	s.steps = s.steps[1:]
	return n, st.err
}

func pattern(n int) string {
	p := make([]byte, n)
	for i := range p {
		p[i] = byte(i*7 + i>>8)
	}
	return string(p)
}

func TestReaderLazy(t *testing.T) {
	src := &script{}
	r := NewReader(src, ReaderOptions{})
	if r.b != nil || r.Len() != 0 || r.Buffered() != nil || r.Discard(1) != 0 || r.Shrink() || r.TakeBuffered() != nil {
		t.Fatal("a new Reader holds a buffer or data")
	}
	if p, err := r.Peek(0); len(p) != 0 || err != nil || src.calls != 0 {
		t.Fatalf("Peek(0) = %q, %v after %d reads", p, err, src.calls)
	}
}

func TestPeekDiscard(t *testing.T) {
	r := NewReader(&script{steps: []step{{"hel", nil}, {"lo wor", nil}, {"ld", nil}}}, ReaderOptions{})
	defer r.Release()
	if p, err := r.Peek(5); string(p) != "hello" || err != nil {
		t.Fatalf("Peek(5) = %q, %v", p, err)
	}
	if string(r.Buffered()) != "hello wor" || r.Discard(6) != 6 {
		t.Fatalf("Buffered = %q", r.Buffered())
	}
	if p, err := r.Peek(5); string(p) != "world" || err != nil {
		t.Fatalf("Peek(5) = %q, %v", p, err)
	}
	if r.Discard(99) != 5 || r.Discard(-1) != 0 || r.Len() != 0 {
		t.Fatal("Discard past the end")
	}
}

func TestDataThenError(t *testing.T) {
	src := &script{steps: []step{{"abc", io.EOF}}}
	r := NewReader(src, ReaderOptions{})
	defer r.Release()
	if err := r.Fill(); err != nil || r.Len() != 3 {
		t.Fatalf("Fill = %v with %d bytes", err, r.Len())
	}
	if p, err := r.Peek(5); string(p) != "abc" || err != io.EOF { // short: check both
		t.Fatalf("Peek(5) = %q, %v", p, err)
	}
	if err := r.Fill(); err != io.EOF || string(r.Buffered()) != "abc" {
		t.Fatalf("Fill = %v, Buffered %q", err, r.Buffered())
	}
	p := make([]byte, 2)
	for _, want := range []string{"ab", "c"} {
		if n, err := r.Read(p); string(p[:n]) != want || err != nil {
			t.Fatalf("Read = %q, %v, want %q", p[:n], err, want)
		}
	}
	if n, err := r.Read(p); n != 0 || err != io.EOF {
		t.Fatalf("Read = %d, %v", n, err)
	}
	if src.calls != 1 {
		t.Fatalf("source read %d times after its error", src.calls)
	}
}

func TestFillReadsOnce(t *testing.T) {
	src := &script{steps: []step{{"a", nil}, {"", nil}, {"b", nil}}}
	r := NewReader(src, ReaderOptions{})
	defer r.Release()
	for i, want := range []string{"a", "a", "ab"} {
		if err := r.Fill(); err != nil || src.calls != i+1 || string(r.Buffered()) != want {
			t.Fatalf("Fill %d: %v, %d reads, %q", i, err, src.calls, r.Buffered())
		}
	}
}

func TestNoProgress(t *testing.T) {
	src := &script{}
	r := NewReader(src, ReaderOptions{})
	defer r.Release()
	if _, err := r.Peek(1); err != io.ErrNoProgress || src.calls != maxEmptyReads {
		t.Fatalf("Peek = %v after %d reads", err, src.calls)
	}
	// The count runs across Fill, Peek and Read, and data resets it.
	for i := 0; i < maxEmptyReads-2; i++ {
		if err := r.Fill(); err != nil {
			t.Fatalf("Fill %d: %v", i, err)
		}
	}
	if n, err := r.Read(make([]byte, 600)); n != 0 || err != nil { // direct read, 99th
		t.Fatalf("Read = %d, %v", n, err)
	}
	if _, err := r.Peek(1); err != io.ErrNoProgress {
		t.Fatalf("Peek = %v, want the 100th empty read to fail", err)
	}
	for i := 0; i < maxEmptyReads-1; i++ {
		r.Fill()
	}
	src.steps = []step{{"x", nil}}
	if err := r.Fill(); err != nil || r.Len() != 1 {
		t.Fatalf("Fill = %v", err)
	}
	if err := r.Fill(); err != nil { // would be the 100th had data not reset the count
		t.Fatalf("Fill after data = %v", err)
	}
}

// Empty reads, then data, then empty reads: only the run after the data
// counts, on the buffered path and on the direct one, and also when the
// data arrives together with an error.
func TestEmptyReadCountResets(t *testing.T) {
	for _, direct := range []bool{false, true} {
		src := &script{}
		r := NewReader(src, ReaderOptions{})
		size := 1
		if direct {
			size = 600 // at least IdleSize: read straight from the source
		}
		p := make([]byte, size)
		read := func(want int, wantErr error) {
			t.Helper()
			if n, err := r.Read(p); n != want || err != wantErr {
				t.Fatalf("direct %v: Read = %d, %v; want %d, %v", direct, n, err, want, wantErr)
			}
		}
		for i := 0; i < maxEmptyReads-1; i++ {
			read(0, nil)
		}
		src.steps = []step{{"x", nil}}
		read(1, nil)
		for i := 0; i < maxEmptyReads-1; i++ {
			read(0, nil) // the 99 before the data no longer count
		}
		read(0, io.ErrNoProgress)

		for i := 0; i < maxEmptyReads-1; i++ {
			read(0, nil)
		}
		src.steps = []step{{"y", io.EOF}}
		read(1, nil)
		read(0, io.EOF) // the sticky error, not io.ErrNoProgress
		r.Release()
	}
}

func TestMax(t *testing.T) {
	r := NewReader(&script{steps: []step{{pattern(3000), nil}}}, ReaderOptions{IdleSize: 100, Max: 1000}) // 1000 is no size class
	defer r.Release()
	if _, err := r.Peek(-1); err != ErrNegativeCount {
		t.Fatalf("Peek(-1) = %v", err)
	}
	if _, err := r.Peek(1001); err != ErrTooLarge {
		t.Fatalf("Peek(Max+1) = %v", err)
	}
	if _, err := r.Peek(10); err != nil || r.b.Cap() != 100 {
		t.Fatalf("Peek(10) = %v, Cap %d", err, r.b.Cap())
	}
	if p, err := r.Peek(1000); string(p) != pattern(3000)[:1000] || err != nil { // grows past the 512 class
		t.Fatalf("Peek(Max): %d bytes, %v", len(p), err)
	}
	if err := r.Fill(); err != ErrTooLarge || r.Len() != 1000 || r.b.Cap() != 1000 {
		t.Fatalf("Fill at Max = %v, Len %d, Cap %d", err, r.Len(), r.b.Cap())
	}
	r.Discard(1)
	if err := r.Fill(); err != nil || r.Len() != 1000 { // compacts, stays at Max
		t.Fatalf("Fill after Discard = %v, Len %d", err, r.Len())
	}
	if string(r.Buffered()) != pattern(3000)[1:1001] {
		t.Fatal("data damaged by compaction")
	}
}

func TestDirectRead(t *testing.T) {
	r := NewReader(&script{steps: []step{{pattern(2000), nil}}}, ReaderOptions{})
	defer r.Release()
	big := make([]byte, 1000)
	if n, err := r.Read(big); n != 1000 || err != nil || r.b != nil {
		t.Fatalf("large Read = %d, %v, buffered through %v", n, err, r.b)
	}
	small := make([]byte, 10)
	if n, err := r.Read(small); n != 10 || err != nil || r.Len() != 502 {
		t.Fatalf("small Read = %d, %v, %d left", n, err, r.Len())
	}
	if n, _ := r.Read(big); n != 502 { // buffered data first, never mixed with a source read
		t.Fatalf("Read = %d", n)
	}
	if string(big[:502]) != pattern(2000)[1010:1512] || string(small) != pattern(2000)[1000:1010] {
		t.Fatal("bytes out of order")
	}
	if n, err := r.Read(nil); n != 0 || err != nil {
		t.Fatalf("Read(nil) = %d, %v", n, err)
	}
}

func TestGrowShrink(t *testing.T) {
	data := pattern(5000)
	r := NewReader(&script{steps: []step{{data[:4000], nil}, {data[4000:4010], nil}, {data[4010:], nil}}}, ReaderOptions{})
	defer r.Release()
	if p, err := r.Peek(4000); string(p) != data[:4000] || err != nil || r.b.Cap() != 4096 {
		t.Fatalf("Peek(4000): %d bytes, %v", len(p), err)
	}
	if r.Shrink() {
		t.Fatal("Shrink with data buffered")
	}
	r.Discard(4000) // empties after a heavy read: the buffer stays
	if r.b == nil {
		t.Fatal("buffer dropped after a heavy read")
	}
	if p, _ := r.Peek(10); string(p) != data[4000:4010] {
		t.Fatalf("Peek(10) = %q", p)
	}
	r.Discard(10) // empties after a light read: the buffer goes
	if r.b != nil {
		t.Fatal("buffer kept after a light read")
	}
	r.Fill()
	r.Discard(r.Len())
	if !r.Shrink() || r.b != nil || r.Shrink() {
		t.Fatal("Shrink of an empty buffer")
	}
}

func TestTakeBuffered(t *testing.T) {
	src := &script{steps: []step{{pattern(100), io.EOF}}}
	r := NewReader(src, ReaderOptions{})
	defer r.Release()
	r.Peek(10)
	r.Discard(10)
	b := r.TakeBuffered()
	if string(b.Bytes()) != pattern(100)[10:] || r.Len() != 0 || r.TakeBuffered() != nil {
		t.Fatalf("took %d bytes, %d left", b.Len(), r.Len())
	}
	b.Release() // the Reader no longer holds it: a second owner would panic here
	if n, err := r.Read(make([]byte, 8)); n != 0 || err != io.EOF || src.calls != 1 {
		t.Fatalf("Read after take = %d, %v, %d source reads", n, err, src.calls)
	}

	r2 := NewReader(&script{steps: []step{{"ab", nil}, {"cd", nil}}}, ReaderOptions{})
	defer r2.Release()
	r2.Fill()
	b = r2.TakeBuffered()
	defer b.Release()
	if p, err := r2.Peek(2); string(b.Bytes()) != "ab" || string(p) != "cd" || err != nil {
		t.Fatalf("took %q, then read %q, %v", b.Bytes(), p, err)
	}
}

func TestRelease(t *testing.T) {
	r := NewReader(&script{steps: []step{{"abc", nil}}}, ReaderOptions{})
	r.Fill()
	r.Release()
	r.Release()
	if _, err := r.Peek(1); err != ErrReleased {
		t.Errorf("Peek = %v", err)
	}
	if err := r.Fill(); err != ErrReleased {
		t.Errorf("Fill = %v", err)
	}
	if _, err := r.Read(make([]byte, 1)); err != ErrReleased {
		t.Errorf("Read = %v", err)
	}
	if r.Len() != 0 || r.Buffered() != nil || r.TakeBuffered() != nil || r.Discard(1) != 0 || r.Shrink() {
		t.Error("a released Reader still holds data")
	}
}

type liar int

func (l liar) Read(p []byte) (int, error)  { return len(p) + int(l), nil }
func (l liar) Write(p []byte) (int, error) { return len(p) + int(l), nil }

func TestReaderInvalidCount(t *testing.T) {
	for _, l := range []liar{1, -9999} {
		for _, size := range []int{1, 1000} { // buffered and direct
			r := NewReader(l, ReaderOptions{})
			for i := 0; i < 2; i++ { // sticky
				if n, err := r.Read(make([]byte, size)); n != 0 || err != ErrInvalidCount {
					t.Errorf("liar %d, Read(%d) = %d, %v", l, size, n, err)
				}
			}
			r.Release()
		}
	}
	mustPanic := func(o ReaderOptions) {
		defer func() {
			if recover() == nil {
				t.Errorf("NewReader(%+v): no panic", o)
			}
		}()
		NewReader(nil, o)
	}
	mustPanic(ReaderOptions{IdleSize: -1})
	mustPanic(ReaderOptions{IdleSize: 100, Max: 50})
}

// FuzzReader checks a Reader against the byte stream it reads: whatever
// the chunking and the calls, bytes come out once and in order.
func FuzzReader(f *testing.F) {
	f.Add([]byte{3, 200, 0, 9}, []byte{0, 5, 1, 2, 2, 7, 3, 0, 4, 0, 0, 255, 1, 255})
	f.Fuzz(func(t *testing.T, chunks, ops []byte) {
		if len(chunks) > 64 {
			chunks = chunks[:64] // keep the stream, and the run, short
		}
		total := 0
		for _, c := range chunks {
			total += int(c) * 8
		}
		stream, src := pattern(total), &script{}
		for at, i := 0, 0; i < len(chunks); i++ {
			n := int(chunks[i]) * 8
			src.steps = append(src.steps, step{stream[at : at+n], nil})
			at += n
		}
		src.steps = append(src.steps, step{"", io.EOF})
		r := NewReader(src, ReaderOptions{IdleSize: 16, Max: 700})
		defer r.Release()
		pos := 0 // stream[:pos] has been consumed
		for i := 0; i+1 < len(ops); i += 2 {
			n := int(ops[i+1]) * 3
			switch ops[i] % 5 {
			case 0:
				p, err := r.Peek(n)
				if err == nil && len(p) != n {
					t.Fatalf("Peek(%d) = %d bytes, nil", n, len(p))
				}
				if err != nil && err != io.EOF && err != ErrTooLarge {
					t.Fatalf("Peek(%d) = %v", n, err)
				}
			case 1:
				pos += r.Discard(n)
			case 2:
				p := make([]byte, n)
				k, err := r.Read(p)
				if string(p[:k]) != stream[pos:pos+k] || (err != nil && err != io.EOF) {
					t.Fatalf("Read(%d) = %d bytes, %v", n, k, err)
				}
				pos += k
			case 3:
				if err := r.Fill(); err != nil && err != io.EOF && err != ErrTooLarge {
					t.Fatalf("Fill = %v", err)
				}
			case 4:
				if b := r.TakeBuffered(); b != nil {
					if string(b.Bytes()) != stream[pos:pos+b.Len()] {
						t.Fatal("TakeBuffered out of order")
					}
					pos += b.Len()
					b.Release()
				}
			}
			if r.Len() > 700 || string(r.Buffered()) != stream[pos:pos+r.Len()] {
				t.Fatalf("after op %d: %d bytes buffered, not the stream at %d", ops[i]%5, r.Len(), pos)
			}
		}
		rest, err := io.ReadAll(r)
		if err != nil || string(rest) != stream[pos:] {
			t.Fatalf("drained %d bytes, %v; want %d", len(rest), err, len(stream)-pos)
		}
	})
}

// sink is a destination that records writes and can cut one short.
type sink struct {
	bytes.Buffer
	writes int
	cutAt  int   // write number (from 1) to cut short; 0 for none
	keep   int   // bytes that write accepts
	err    error // and the error it returns
}

func (s *sink) Write(p []byte) (int, error) {
	if s.writes++; s.writes == s.cutAt {
		s.Buffer.Write(p[:s.keep])
		return s.keep, s.err
	}
	return s.Buffer.Write(p)
}

// queue adds "aaaa" (owned), "bbbb" (shared) and "cccc" (owned).
func queue(w *Writer) (owned []*buf.Buf) {
	a, c := buf.Copy([]byte("aaaa")), buf.Copy([]byte("cccc"))
	w.Add(a)
	w.AddShared([]byte("bbbb"))
	w.Add(c)
	return []*buf.Buf{a, c}
}

// drained fails unless w is empty, holds no reference, and released owned.
func drained(t *testing.T, w *Writer, owned []*buf.Buf) {
	t.Helper()
	if b, n := w.Pending(); b != 0 || n != 0 || w.tmp != nil {
		t.Errorf("Pending = %d, %d", b, n)
	}
	for _, p := range w.vec[:cap(w.vec)] {
		if p != nil {
			t.Error("a segment is still referenced")
		}
	}
	for _, b := range w.own[:cap(w.own)] {
		if b != nil {
			t.Error("an owned buffer is still referenced")
		}
	}
	for _, b := range owned {
		if b.Cap() != 0 {
			t.Error("an owned buffer was not released")
		}
	}
}

func TestFlush(t *testing.T) {
	boom := errors.New("boom")
	for _, c := range []struct {
		name       string
		cutAt      int
		keep       int
		err        error
		want       string
		wantErr    error
		wantWrites int
	}{
		{"all", 0, 0, nil, "aaaabbbbcccc", nil, 3},
		{"error after part", 2, 1, boom, "aaaab", boom, 2},
		{"error at once", 1, 0, boom, "", boom, 1},
		{"short without error", 2, 3, nil, "aaaabbb", io.ErrShortWrite, 2},
		{"nothing without error", 1, 0, nil, "", io.ErrShortWrite, 1},
	} {
		s := &sink{cutAt: c.cutAt, keep: c.keep, err: c.err}
		w := NewWriter(s)
		owned := queue(w)
		if b, n := w.Pending(); b != 3 || n != 12 {
			t.Errorf("%s: Pending = %d, %d", c.name, b, n)
		}
		n, err := w.Flush()
		if n != int64(len(c.want)) || err != c.wantErr || s.String() != c.want || s.writes != c.wantWrites {
			t.Errorf("%s: Flush = %d, %v; wrote %q in %d writes", c.name, n, err, s.String(), s.writes)
		}
		drained(t, w, owned)
	}
}

func TestFlushInvalidCount(t *testing.T) {
	for _, l := range []liar{1, -9999} {
		w := NewWriter(l)
		owned := queue(w)
		if n, err := w.Flush(); n != 0 || err != ErrInvalidCount {
			t.Errorf("liar %d: Flush = %d, %v", l, n, err)
		}
		drained(t, w, owned)
	}
}

func TestWriterReuse(t *testing.T) {
	s := &sink{}
	w := NewWriter(s)
	if n, err := w.Flush(); n != 0 || err != nil || s.writes != 0 {
		t.Fatalf("empty Flush = %d, %v", n, err)
	}
	w.AddShared(nil)
	empty := buf.Get(8)
	w.Add(empty) // owned, nothing to send
	if b, _ := w.Pending(); b != 0 {
		t.Fatalf("empty segments queued: %d", b)
	}
	w.Flush()
	drained(t, w, []*buf.Buf{empty})

	owned := queue(w)
	w.Discard()
	drained(t, w, owned)
	if s.Len() != 0 {
		t.Fatal("Discard wrote")
	}
	owned = queue(w)
	if n, err := w.Flush(); n != 12 || err != nil || s.String() != "aaaabbbbcccc" {
		t.Fatalf("Flush after Discard = %d, %v, %q", n, err, s.String())
	}
	drained(t, w, owned)
}

// pipe returns the two ends of a loopback TCP connection.
func pipe(tb testing.TB) (*net.TCPConn, *net.TCPConn) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatal(err)
	}
	defer ln.Close()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		tb.Fatal(err)
	}
	s, err := ln.Accept()
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { c.Close(); s.Close() })
	return c.(*net.TCPConn), s.(*net.TCPConn)
}

// counted hides the connection type, as TLS or a counter would.
type counted struct {
	io.Writer
	writes int
}

func (c *counted) Write(p []byte) (int, error) { c.writes++; return c.Writer.Write(p) }

func TestFlushTCP(t *testing.T) {
	c, s := pipe(t)
	wrapped := &counted{Writer: c}
	for _, dst := range []io.Writer{c, wrapped} { // writev, then one Write per segment
		w := NewWriter(dst)
		var want []byte
		var owned []*buf.Buf
		for i := 0; i < 50; i++ {
			b := buf.Copy([]byte(fmt.Sprintf("message %03d;", i)))
			owned = append(owned, b)
			want = append(want, b.Bytes()...)
			w.Add(b)
			w.AddShared([]byte("shared;"))
			want = append(want, "shared;"...)
		}
		if n, err := w.Flush(); n != int64(len(want)) || err != nil {
			t.Fatalf("Flush = %d, %v", n, err)
		}
		drained(t, w, owned)
		got := make([]byte, len(want))
		if _, err := io.ReadFull(s, got); err != nil || !bytes.Equal(got, want) {
			t.Fatalf("peer read %v, equal %v", err, bytes.Equal(got, want))
		}
	}
	if wrapped.writes != 100 {
		t.Fatalf("wrapper saw %d writes for 100 segments", wrapped.writes)
	}
	c.Close()
	w := NewWriter(c)
	owned := queue(w)
	if _, err := w.Flush(); err == nil {
		t.Fatal("Flush on a closed connection succeeded")
	}
	drained(t, w, owned)
}

// One Flush per batch against one Write per message, 64-byte messages.
func BenchmarkFlush(b *testing.B) {
	msg := make([]byte, 64)
	for _, batch := range []int{1, 8, 32} {
		for _, mode := range []string{"write", "flush"} {
			b.Run(fmt.Sprintf("%s/batch=%d", mode, batch), func(b *testing.B) {
				c, s := pipe(b)
				go io.Copy(io.Discard, s)
				w := NewWriter(c)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					for j := 0; j < batch; j++ {
						m := buf.Copy(msg)
						if mode == "flush" {
							w.Add(m)
							continue
						}
						c.Write(m.Bytes())
						m.Release()
					}
					if _, err := w.Flush(); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// frames is an endless stream of one frame repeated.
type frames struct {
	frame []byte
	off   int
}

func (f *frames) Read(p []byte) (int, error) {
	n := copy(p, f.frame[f.off:])
	f.off = (f.off + n) % len(f.frame)
	return n, nil
}

// A length-prefixed frame per iteration: Peek the header, Peek the frame,
// Discard.
func BenchmarkReadFrames(b *testing.B) {
	for _, size := range []int{64, 1400, 16000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			frame := make([]byte, size)
			frame[0], frame[1] = byte(size>>8), byte(size)
			r := NewReader(&frames{frame: frame}, ReaderOptions{})
			defer r.Release()
			b.ReportAllocs()
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				h, _ := r.Peek(2)
				n := int(h[0])<<8 | int(h[1])
				if _, err := r.Peek(n); err != nil || n != size {
					b.Fatal(err)
				}
				r.Discard(n)
			}
		})
	}
}
