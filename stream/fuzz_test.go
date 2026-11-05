package stream

import (
	"bytes"
	"testing"

	"github.com/ninepeach/go-netx/buf"
	"github.com/ninepeach/go-netx/limit"
)

// A random stream split into random chunks, consumed by a random sequence
// of Peek/Discard/Read/Fill: the bytes handed out are exactly the stream,
// in order, and every borrowed buffer goes back to the pool.
//
// Checks: NF-20
func FuzzReader(f *testing.F) {
	f.Add([]byte("hello, stream"), []byte{3, 1, 9}, []byte{0, 5, 1, 2, 3, 2, 0, 40})
	f.Add(bytes.Repeat([]byte{7}, 5000), []byte{200, 1, 255}, []byte{0, 255, 1, 255, 0, 30})
	f.Fuzz(readerModel)
}

func readerModel(t *testing.T, data, cuts, ops []byte) {
	var parts [][]byte
	rest := data
	for i := 0; len(rest) > 0; i++ {
		n := 1
		if len(cuts) > 0 {
			n = int(cuts[i%len(cuts)])*17 + 1
		}
		n = min(n, len(rest))
		parts = append(parts, rest[:n])
		rest = rest[n:]
	}
	budget := limit.NewBudget(1 << 20)
	r := NewReader(&chunks{parts: parts}, buf.NewPool(budget), ReaderOptions{IdleSize: 64, Max: 8192})
	pos := 0 // bytes of data handed out so far
	take := func(got []byte) {
		if pos+len(got) > len(data) || !bytes.Equal(got, data[pos:pos+len(got)]) {
			t.Fatalf("stream corrupted at byte %d", pos)
		}
		pos += len(got)
	}
	p := make([]byte, 200)
	for i := 0; i+1 < len(ops); i += 2 {
		arg := int(ops[i+1]) * 33
		switch ops[i] % 4 {
		case 0: // Peek arg bytes and consume them
			got, err := r.Peek(arg)
			if err == nil && len(got) != arg {
				t.Fatalf("Peek(%d) returned %d bytes", arg, len(got))
			}
			take(got)
			r.Discard(len(got))
		case 1: // Read
			n, _ := r.Read(p[:arg%200+1])
			take(p[:n])
		case 2: // Fill then take everything buffered
			r.Fill()
			take(r.Buffered())
			r.Discard(r.Len())
		case 3: // take a little of what is buffered
			b := r.Buffered()
			k := min(arg%50, len(b))
			take(b[:k])
			r.Discard(k)
		}
		if r.Len() > 8192 {
			t.Fatalf("buffered %d > Max", r.Len())
		}
	}
	r.Release()
	if budget.Used() != 0 {
		t.Fatalf("budget holds %d after Release", budget.Used())
	}
}
