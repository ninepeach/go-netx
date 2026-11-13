package buf

import (
	"bytes"
	"math"
	"runtime"
	"runtime/debug"
	"testing"
)

func mustPanic(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: no panic", name)
		}
	}()
	f()
}

func TestClasses(t *testing.T) {
	for _, c := range []struct{ n, mem int }{
		{0, 64}, {1, 64}, {64, 64}, {65, 128}, {1500, 2048},
		{Large - 1, Large}, {Large, Large}, {Large + 1, maxPooled},
		{maxPooled, maxPooled}, {maxPooled + 1, maxPooled + 1},
	} {
		if got := Capacity(c.n); got != c.mem {
			t.Errorf("Capacity(%d) = %d, want %d", c.n, got, c.mem)
		}
		b := Get(c.n)
		if b.Len() != 0 || b.Cap() != c.n || len(b.FreeBytes()) != c.n || len(b.data) != c.mem {
			t.Errorf("Get(%d): Len %d Cap %d free %d mem %d", c.n, b.Len(), b.Cap(), len(b.FreeBytes()), len(b.data))
		}
		b.Release()
	}
	if Capacity(65537) != 73728 || Capacity(73728) != 73728 || Capacity(73729) != 73729 {
		t.Fatalf("72KiB boundaries: %d, %d, %d", Capacity(65537), Capacity(73728), Capacity(73729))
	}
	b := GetHeadroom(1, Large)
	if len(b.data) != maxPooled || b.Cap() != Large+1 || b.start != 1 || b.class != powerClasses {
		t.Fatalf("GetHeadroom across 64KiB: mem %d Cap %d start %d class %d", len(b.data), b.Cap(), b.start, b.class)
	}
	b.Release()
}

func TestCursors(t *testing.T) {
	b := GetHeadroom(4, 8)
	defer b.Release()
	if b.Cap() != 12 || len(b.FreeBytes()) != 8 {
		t.Fatalf("Cap %d free %d", b.Cap(), len(b.FreeBytes()))
	}
	copy(b.Extend(5), "hello")
	copy(b.ExtendHeader(2), "ab")
	if got := string(b.Bytes()); got != "abhello" {
		t.Fatalf("got %q", got)
	}
	b.Advance(2) // strip the header; it is headroom again
	b.Truncate(4)
	copy(b.ExtendHeader(4), "wxyz")
	if got := string(b.Bytes()); got != "wxyzhell" || len(b.FreeBytes()) != 4 {
		t.Fatalf("got %q, free %d", got, len(b.FreeBytes()))
	}
	b.Reset()
	if b.Len() != 0 || len(b.FreeBytes()) != 8 {
		t.Fatalf("after Reset: Len %d free %d", b.Len(), len(b.FreeBytes()))
	}
	copy(b.ExtendHeader(4), "head") // Reset restored the headroom
}

func TestCopy(t *testing.T) {
	src := []byte("datagram")
	b := Copy(src)
	defer b.Release()
	src[0] = 'X'
	if string(b.Bytes()) != "datagram" || b.Cap() != 8 {
		t.Fatalf("got %q, Cap %d", b.Bytes(), b.Cap())
	}
}

func TestPanics(t *testing.T) {
	mustPanic(t, "Get(-1)", func() { Get(-1) })
	mustPanic(t, "GetHeadroom(-1, 1)", func() { GetHeadroom(-1, 1) })
	mustPanic(t, "GetHeadroom overflow", func() { GetHeadroom(math.MaxInt, 1) })
	mustPanic(t, "Capacity(-1)", func() { Capacity(-1) })

	b := GetHeadroom(2, 4)
	defer b.Release()
	b.Extend(3)
	mustPanic(t, "Extend past Cap", func() { b.Extend(2) })
	mustPanic(t, "Extend(-1)", func() { b.Extend(-1) })
	mustPanic(t, "ExtendHeader past headroom", func() { b.ExtendHeader(3) })
	mustPanic(t, "ExtendHeader(-1)", func() { b.ExtendHeader(-1) })
	mustPanic(t, "Advance past Len", func() { b.Advance(4) })
	mustPanic(t, "Advance(-1)", func() { b.Advance(-1) })
	mustPanic(t, "Truncate past Len", func() { b.Truncate(4) })
	mustPanic(t, "Truncate(-1)", func() { b.Truncate(-1) })
	if b.Len() != 3 || len(b.FreeBytes()) != 1 {
		t.Fatalf("a refused call moved a cursor: Len %d free %d", b.Len(), len(b.FreeBytes()))
	}
}

func TestRelease(t *testing.T) {
	for _, n := range []int{100, Large + 1, maxPooled + 1} { // ordinary pool, 72KiB pool, direct allocation
		b := GetHeadroom(8, n)
		b.Extend(10)
		b.Release()
		if b.Len() != 0 || b.Cap() != 0 || len(b.Bytes()) != 0 || len(b.FreeBytes()) != 0 {
			t.Errorf("size %d: not empty after Release", n)
		}
		mustPanic(t, "Extend after Release", func() { b.Extend(1) })
		mustPanic(t, "ExtendHeader after Release", func() { b.ExtendHeader(1) })
		mustPanic(t, "double Release", b.Release)
	}
}

// A buffer obtained on one goroutine is released on another.
func TestHandoff(t *testing.T) {
	ch := make(chan *Buf, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for b := range ch {
			if b.Bytes()[0] != byte(b.Len()) {
				t.Errorf("wrong data in buffer of %d bytes", b.Len())
			}
			b.Release()
		}
	}()
	for i := 0; i < 2000; i++ {
		b := Get(1 + i%200)
		b.Extend(b.Cap())[0] = byte(b.Cap())
		ch <- b
	}
	close(ch)
	<-done
}

func raceEnabled() bool {
	bi, _ := debug.ReadBuildInfo()
	for _, s := range bi.Settings {
		if s.Key == "-race" {
			return s.Value == "true"
		}
	}
	return false
}

func TestNoAlloc(t *testing.T) {
	if raceEnabled() {
		t.Skip("sync.Pool drops items under the race detector")
	}
	for _, n := range []int{64, 1500, Large, maxPooled} {
		if a := testing.AllocsPerRun(1000, func() {
			b := GetHeadroom(16, n-16)
			b.Extend(1)
			b.ExtendHeader(16)
			b.Release()
		}); a != 0 {
			t.Errorf("size %d: %v allocs per Get/Release", n, a)
		}
	}
}

// FuzzCursors drives a Buf and a plain-slice model with the same
// operations; an operation the model cannot do must panic.
func FuzzCursors(f *testing.F) {
	f.Add(uint8(4), uint16(100), []byte{0, 10, 1, 4, 2, 3, 3, 5, 4, 0})
	f.Add(uint8(0), uint16(0), []byte{0, 1, 1, 1})
	f.Fuzz(func(t *testing.T, head uint8, size uint16, ops []byte) {
		b := GetHeadroom(int(head), int(size))
		defer b.Release()
		var want []byte
		room, free := int(head), int(size) // headroom and free space in the model
		next := byte(1)
		fill := func(p []byte) {
			for i := range p {
				p[i] = next
				next++
			}
		}
		for i := 0; i+1 < len(ops); i += 2 {
			op, n := ops[i]%5, int(ops[i+1])
			ok := true
			switch op {
			case 0:
				if ok = n <= free; ok {
					fill(b.Extend(n))
					want = append(want, b.Bytes()[len(want):]...)
					free -= n
				}
			case 1:
				if ok = n <= room; ok {
					fill(b.ExtendHeader(n))
					want = append(append([]byte{}, b.Bytes()[:n]...), want...)
					room -= n
				}
			case 2:
				if ok = n <= len(want); ok {
					b.Advance(n)
					want = want[n:]
					room += n
				}
			case 3:
				if ok = n <= len(want); ok {
					b.Truncate(n)
					free += len(want) - n
					want = want[:n]
				}
			case 4:
				b.Reset()
				want, room, free = nil, int(head), int(size)
			}
			if !ok {
				mustPanic(t, "out-of-range op", func() {
					[]func(int){func(n int) { b.Extend(n) }, func(n int) { b.ExtendHeader(n) }, b.Advance, b.Truncate}[op](n)
				})
			}
			if !bytes.Equal(b.Bytes(), want) || len(b.FreeBytes()) != free || b.Cap() != int(head)+int(size) {
				t.Fatalf("op %d(%d): Len %d free %d, model Len %d free %d", op, n, b.Len(), len(b.FreeBytes()), len(want), free)
			}
		}
	})
}

func BenchmarkGetRelease(b *testing.B) {
	for _, s := range []struct {
		name string
		n    int
	}{{"1500", 1500}, {"16K", 16 << 10}, {"64K", Large}, {"72K", maxPooled}, {"over", maxPooled + 1}} {
		b.Run(s.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				Get(s.n).Release()
			}
		})
	}
}

// Payload, then two headers in front of it.
func BenchmarkHeaders(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		x := GetHeadroom(32, 1400)
		x.Extend(1400)[0] = 1
		x.ExtendHeader(16)[0] = 2
		x.ExtendHeader(16)[0] = 3
		x.Release()
	}
}

func BenchmarkHandoff(b *testing.B) {
	ch := make(chan *Buf, 256)
	done := make(chan struct{})
	go func() {
		for x := range ch {
			x.Release()
		}
		close(done)
	}()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ch <- Get(1500)
	}
	close(ch)
	<-done
}

func BenchmarkParallel(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			Get(1500).Release()
		}
	})
}

func BenchmarkGetReleaseWithGC(b *testing.B) {
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		if i%256 == 0 {
			// Two GC cycles evict idle entries from sync.Pool.
			runtime.GC()
			runtime.GC()
		}
		Get(maxPooled).Release()
	}
}
