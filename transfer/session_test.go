package transfer

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/ninepeach/go-netx/buf"
)

type sessionSink struct {
	bytes.Buffer
	extended int
}

func (w *sessionSink) WriteBuffer(b *buf.Buf) (int, error) {
	w.extended++
	return w.Write(b.Bytes())
}

func TestCopySession(t *testing.T) {
	dst := new(sessionSink)
	s := NewCopySession(dst, strings.NewReader("abcdef"), 4)
	for _, want := range []int{4, 2} {
		n, err := s.Step()
		if n != want || err != nil {
			t.Fatalf("step = %d, %v", n, err)
		}
	}
	if dst.extended != 2 || dst.String() != "abcdef" || s.Transferred() != 6 {
		t.Fatal("wrong stream or counters")
	}
	for range 2 {
		if n, err := s.Step(); n != 0 || err != io.EOF {
			t.Fatalf("terminal step = %d, %v", n, err)
		}
	}
}

func BenchmarkCopySession(b *testing.B) {
	var src bytes.Reader
	data := make([]byte, 4096)
	s := NewCopySession(io.Discard, &src, len(data))
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		src.Reset(data)
		if n, err := s.Step(); n != len(data) || err != nil {
			b.Fatalf("step = %d, %v", n, err)
		}
	}
}

type partialSessionWriter struct{}

func (partialSessionWriter) Write(p []byte) (int, error) { return len(p) / 2, io.ErrClosedPipe }

func TestCopySessionPartialTerminal(t *testing.T) {
	s := NewCopySession(partialSessionWriter{}, strings.NewReader("abcdefgh"), 4)
	if n, err := s.Step(); n != 2 || err != io.ErrClosedPipe {
		t.Fatalf("step = %d, %v", n, err)
	}
	if n, err := s.Step(); n != 0 || err != io.ErrClosedPipe || s.Transferred() != 2 {
		t.Fatalf("terminal step = %d, %v, total %d", n, err, s.Transferred())
	}
}

type benchExtended struct{ reader bytes.Reader }

func (r *benchExtended) Read(p []byte) (int, error) { return r.reader.Read(p) }

func (r *benchExtended) ReadBuffer(b *buf.Buf) (int, error) { return b.ReadOnceFrom(r) }

func BenchmarkMemoryCopy(b *testing.B) {
	data := make([]byte, 256<<10)
	for _, test := range []struct {
		name string
		copy func(io.Writer, io.Reader) (int64, error)
	}{
		{"standard", io.Copy}, {"pooled", CopyBuffered}, {"extended", Copy},
	} {
		b.Run(test.name, func(b *testing.B) {
			var input benchExtended
			var src io.Reader = readOnly{&input}
			if test.name == "extended" {
				src = &input
			}
			dst := writeOnly{io.Discard}
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				input.reader.Reset(data)
				if n, err := test.copy(dst, src); n != int64(len(data)) || err != nil {
					b.Fatalf("copy = %d, %v", n, err)
				}
			}
		})
	}
}
