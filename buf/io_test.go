package buf

import (
	"bytes"
	"errors"
	"io"
	"math"
	"strings"
	"testing"
)

var (
	_ io.ReadWriter = (*Buf)(nil)
	_ io.ByteReader = (*Buf)(nil)
	_ io.ByteWriter = (*Buf)(nil)
	_ io.WriterTo   = (*Buf)(nil)
)

func TestReadWrite(t *testing.T) {
	b := GetHeadroom(2, 4)
	defer b.Release()
	if n, err := b.Write([]byte("ab")); n != 2 || err != nil {
		t.Fatalf("write = %d, %v", n, err)
	}
	if err := b.WriteByte('c'); err != nil {
		t.Fatal(err)
	}
	if n, err := b.Write([]byte("def")); n != 1 || err != io.ErrShortBuffer {
		t.Fatalf("short write = %d, %v", n, err)
	}
	if err := b.WriteByte('e'); err != io.ErrShortBuffer {
		t.Fatal(err)
	}
	if n, err := b.Write(nil); n != 0 || err != nil {
		t.Fatalf("empty write = %d, %v", n, err)
	}
	mustPanic(t, "negative read", func() { b.ReadBytes(-1) })
	if p, err := b.ReadBytes(math.MaxInt); p != nil || err != io.ErrUnexpectedEOF || string(b.Bytes()) != "abcd" {
		t.Fatalf("oversized read = %q, %v, remaining %q", p, err, b.Bytes())
	}
	original := b.Bytes()
	p, err := b.ReadBytes(2)
	if err != nil || string(p) != "ab" || cap(p) != 2 || &p[0] != &original[0] {
		t.Fatalf("borrow = %q, %v", p, err)
	}
	if c, err := b.ReadByte(); c != 'c' || err != nil {
		t.Fatalf("byte = %q, %v", c, err)
	}
	var out [2]byte
	if n, err := b.Read(out[:]); n != 1 || err != nil || out[0] != 'd' {
		t.Fatalf("read = %d, %v, %q", n, err, out)
	}
	if n, err := b.Read(out[:]); n != 0 || err != io.EOF {
		t.Fatalf("EOF = %d, %v", n, err)
	}
	if n, err := b.Read(nil); n != 0 || err != nil {
		t.Fatalf("empty read = %d, %v", n, err)
	}
	if _, err := b.ReadByte(); err != io.EOF {
		t.Fatal(err)
	}
	if p, err := b.ReadBytes(1); p != nil || err != io.EOF {
		t.Fatalf("empty read bytes = %q, %v", p, err)
	}
	if p, err := b.ReadBytes(0); len(p) != 0 || cap(p) != 0 || err != nil {
		t.Fatalf("zero read bytes = %q, %v", p, err)
	}
	b.Reset()
	if b.Len() != 0 || len(b.FreeBytes()) != 4 {
		t.Fatal("reset did not restore headroom")
	}
}

func TestExternalAndRelease(t *testing.T) {
	data := []byte("abc!")
	for _, wrap := range []struct {
		makeBuf func([]byte) *Buf
		length  int
	}{{As, 3}, {With, 0}} {
		b := wrap.makeBuf(data[:3])
		if b.Len() != wrap.length || b.Cap() != 3 || cap(b.Bytes()) != wrap.length || cap(b.FreeBytes()) != 3-wrap.length {
			t.Fatal("incorrect external bounds")
		}
		b.Reset()
		if n, err := b.Write([]byte("xyzQ")); n != 3 || err != io.ErrShortBuffer {
			t.Fatalf("external write = %d, %v", n, err)
		}
		if &b.Bytes()[0] != &data[0] {
			t.Fatal("external storage was copied")
		}
		b.Release()
		if string(data) != "xyz!" || b.data != nil {
			t.Fatal("release modified or retained external memory")
		}
		mustPanic(t, "external double release", b.Release)
		mustPanic(t, "released read", func() { b.Read(nil) })
		mustPanic(t, "released empty extend", func() { b.Extend(0) })
	}
	var zero Buf
	zero.Release()
	mustPanic(t, "zero double release", zero.Release)
	b := Get(3)
	defer b.Release()
	copyOfBuf := *b
	mustPanic(t, "copied release", copyOfBuf.Release)
	if len(b.data) != 64 {
		t.Fatal("pool storage corrupted")
	}
	if p := b.FreeBytes(); cap(p) != 3 {
		t.Fatal("exposed pool class slack")
	}
}

type readFunc func([]byte) (int, error)

func (f readFunc) Read(p []byte) (int, error) { return f(p) }

type writeFunc func([]byte) (int, error)

func (f writeFunc) Write(p []byte) (int, error) { return f(p) }

func TestIOTransfers(t *testing.T) {
	b := Get(8)
	defer b.Release()
	unexpected := errors.New("transport failed")
	if n, err := b.ReadOnceFrom(readFunc(func(p []byte) (int, error) { p[0] = 'a'; return 1, unexpected })); n != 1 || err != unexpected {
		t.Fatalf("read once = %d, %v", n, err)
	}
	invalid := readFunc(func(p []byte) (int, error) { return len(p) + 1, nil })
	if n, err := b.ReadFullFrom(invalid, 1); n != 0 || err != errInvalidCount || string(b.Bytes()) != "a" {
		t.Fatalf("invalid reader = %d, %v, %q", n, err, b.Bytes())
	}
	if n, err := b.ReadFullFrom(strings.NewReader("bc"), 3); n != 2 || err != io.ErrUnexpectedEOF {
		t.Fatalf("short read = %d, %v", n, err)
	}
	if n, err := b.ReadFullFrom(nil, math.MaxInt); n != 0 || err != io.ErrShortBuffer {
		t.Fatalf("oversized transfer = %d, %v", n, err)
	}
	if n, err := b.ReadFullFrom(nil, 0); n != 0 || err != nil {
		t.Fatalf("empty transfer = %d, %v", n, err)
	}
	if n, err := b.ReadFullFrom(readFunc(func([]byte) (int, error) { return 0, nil }), 1); n != 0 || err != io.ErrNoProgress {
		t.Fatalf("stalled reader = %d, %v", n, err)
	}
	if n, err := b.WriteTo(writeFunc(func([]byte) (int, error) { return -1, nil })); n != 0 || err != errInvalidCount || string(b.Bytes()) != "abc" {
		t.Fatalf("invalid writer = %d, %v, %q", n, err, b.Bytes())
	}
	if n, err := b.WriteTo(writeFunc(func([]byte) (int, error) { return 1, unexpected })); n != 1 || err != unexpected || string(b.Bytes()) != "bc" {
		t.Fatalf("partial error = %d, %v, %q", n, err, b.Bytes())
	}
	if n, err := b.WriteTo(writeFunc(func([]byte) (int, error) { return 1, nil })); n != 1 || err != io.ErrShortWrite || string(b.Bytes()) != "c" {
		t.Fatalf("partial write = %d, %v, %q", n, err, b.Bytes())
	}
	var dst bytes.Buffer
	if n, err := io.Copy(&dst, b); n != 1 || err != nil || dst.String() != "c" || b.Len() != 0 {
		t.Fatalf("copy = %d, %v, %q", n, err, dst.String())
	}
	if n, err := b.WriteTo(nil); n != 0 || err != nil {
		t.Fatalf("empty write = %d, %v", n, err)
	}
}

func BenchmarkReadWrite(b *testing.B) {
	var payload, out [1500]byte
	packet := Get(len(payload))
	defer packet.Release()
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for b.Loop() {
		packet.Reset()
		if _, err := packet.Write(payload[:]); err != nil {
			b.Fatal(err)
		}
		if _, err := packet.Read(out[:]); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadBytes(b *testing.B) {
	packet := Get(1500)
	defer packet.Release()
	b.ReportAllocs()
	for b.Loop() {
		packet.Reset()
		packet.Extend(1500)
		if _, err := packet.ReadBytes(1500); err != nil {
			b.Fatal(err)
		}
	}
}
