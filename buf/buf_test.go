package buf

import (
	"testing"

	"github.com/ninepeach/go-netx/limit"
)

func TestGetClasses(t *testing.T) {
	p := NewPool(nil)
	for _, tc := range []struct{ n, cap int }{
		{0, Small}, {1500, Small}, {Small + 1, Medium}, {Large, Large}, {Large + 1, Large + 1},
	} {
		b := p.Get(tc.n)
		if len(b.B) != tc.n || cap(b.full) != tc.cap {
			t.Fatalf("Get(%d): len=%d cap=%d want cap %d", tc.n, len(b.B), cap(b.full), tc.cap)
		}
		b.Release()
	}
}

func TestBudgetChargesCapacity(t *testing.T) {
	budget := limit.NewBudget(Small * 2)
	p := NewPool(budget)
	a, b := p.Get(10), p.Get(10)
	if a == nil || b == nil {
		t.Fatal("expected two small buffers")
	}
	if p.Get(1) != nil {
		t.Fatal("budget exceeded")
	}
	a.Release()
	b.Release()
	if budget.Used() != 0 {
		t.Fatalf("used=%d", budget.Used())
	}
}

func TestCopy(t *testing.T) {
	p := NewPool(nil)
	src := []byte("hello")
	b := p.Copy(src)
	src[0] = 'x'
	if string(b.B) != "hello" {
		t.Fatal("copy aliases source")
	}
	b.Release()
}

func TestDoubleReleasePanicsInDebug(t *testing.T) {
	if !Debug {
		t.Skip("needs -tags netxdebug")
	}
	p := NewPool(nil)
	b := p.Get(1)
	b.Release()
	defer func() {
		if recover() == nil {
			t.Fatal("double Release did not panic")
		}
	}()
	b.Release()
}

func BenchmarkGetRelease(b *testing.B) {
	p := NewPool(nil)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			p.Get(1200).Release()
		}
	})
}

func TestReleaseClearsViewAndRefundsUnpooledCapacity(t *testing.T) {
	const size = Large + 1
	budget := limit.NewBudget(size)
	p := NewPool(budget)
	b := p.Get(size)
	if b == nil {
		t.Fatal("expected unpooled buffer")
	}
	if budget.Used() != size {
		t.Fatalf("used=%d want=%d", budget.Used(), size)
	}
	b.Release()
	if b.B != nil {
		t.Fatal("Release did not clear the public buffer view")
	}
	if budget.Used() != 0 {
		t.Fatalf("budget not refunded: used=%d", budget.Used())
	}
}

func TestBudgetRejectsLargeBufferWithoutPartialCharge(t *testing.T) {
	budget := limit.NewBudget(Large)
	p := NewPool(budget)
	if b := p.Get(Large + 1); b != nil {
		b.Release()
		t.Fatal("buffer larger than remaining budget was allocated")
	}
	if budget.Used() != 0 {
		t.Fatalf("failed Get charged budget: used=%d", budget.Used())
	}
}

func TestGetNegativeSizePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Get(-1) did not panic")
		}
	}()
	NewPool(nil).Get(-1)
}
