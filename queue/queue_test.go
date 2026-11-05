package queue

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRejectKeepsOwnership(t *testing.T) {
	r := New(Options[int]{Cap: 2})
	if !r.Push(1) || !r.Push(2) || r.Push(3) {
		t.Fatal("reject policy")
	}
	if s := r.Stats(); s.Rejected != 1 || s.Len != 2 {
		t.Fatalf("%+v", s)
	}
}

func TestDropOldestCallsOnDrop(t *testing.T) {
	var dropped []int
	r := New(Options[int]{Cap: 2, Policy: DropOldest, OnDrop: func(v int) { dropped = append(dropped, v) }})
	for i := 1; i <= 4; i++ {
		r.Push(i)
	}
	if len(dropped) != 2 || dropped[0] != 1 || dropped[1] != 2 {
		t.Fatalf("dropped=%v", dropped)
	}
	if v, _ := r.Pop(); v != 3 {
		t.Fatalf("pop=%d", v)
	}
}

func TestCapRoundsUp(t *testing.T) {
	if c := New(Options[int]{Cap: 5}).Cap(); c != 8 {
		t.Fatalf("cap=%d", c)
	}
}

func TestPushWaitUnblocksOnPop(t *testing.T) {
	r := New(Options[int]{Cap: 1})
	r.Push(1)
	done := make(chan error)
	go func() { done <- r.PushWait(context.Background(), 2) }()
	time.Sleep(10 * time.Millisecond)
	r.Pop()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if v, _ := r.Pop(); v != 2 {
		t.Fatal("waited item lost")
	}
}

func TestPushWaitContext(t *testing.T) {
	r := New(Options[int]{Cap: 1})
	r.Push(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := r.PushWait(ctx, 2); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestCloseDrainsThenErrClosed(t *testing.T) {
	r := New(Options[int]{Cap: 4})
	r.Push(1)
	r.Close()
	if r.Push(2) {
		t.Fatal("push after close")
	}
	if v, err := r.PopWait(context.Background()); err != nil || v != 1 {
		t.Fatal("remaining item lost")
	}
	if _, err := r.PopWait(context.Background()); err != ErrClosed {
		t.Fatal(err)
	}
}

func TestCloseWakesWaiters(t *testing.T) {
	r := New(Options[int]{Cap: 1})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r.PopWait(context.Background()) }()
	}
	time.Sleep(10 * time.Millisecond)
	r.Close()
	wg.Wait()
}

func TestDrainReleases(t *testing.T) {
	n := 0
	r := New(Options[int]{Cap: 4, OnDrop: func(int) { n++ }})
	r.Push(1)
	r.Push(2)
	r.Close()
	if r.Drain() != 2 || n != 2 {
		t.Fatal("drain")
	}
}

func TestConcurrentNoLoss(t *testing.T) {
	r := New(Options[int]{Cap: 64})
	const producers, each = 8, 2000
	var wg sync.WaitGroup
	for p := 0; p < producers; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				if err := r.PushWait(context.Background(), 1); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	sum := make(chan int)
	go func() {
		total := 0
		for {
			v, err := r.PopWait(context.Background())
			if err != nil {
				sum <- total
				return
			}
			total += v
		}
	}()
	wg.Wait()
	r.Close()
	if got := <-sum; got != producers*each {
		t.Fatalf("got %d", got)
	}
}

func BenchmarkPushPop(b *testing.B) {
	r := New(Options[int]{Cap: 1024})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r.Push(i)
		r.Pop()
	}
}

func TestCloseWakesBlockedProducer(t *testing.T) {
	r := New(Options[int]{Cap: 1})
	if !r.Push(1) {
		t.Fatal("initial push failed")
	}
	result := make(chan error, 1)
	go func() { result <- r.PushWait(context.Background(), 2) }()
	time.Sleep(10 * time.Millisecond)
	r.Close()
	select {
	case err := <-result:
		if err != ErrClosed {
			t.Fatalf("PushWait error=%v, want ErrClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not wake blocked producer")
	}
	if got := r.Stats(); got.Len != 1 || got.Rejected != 1 {
		t.Fatalf("unexpected stats after rejected producer: %+v", got)
	}
}

func TestCloseRacingPushPreservesBound(t *testing.T) {
	r := New(Options[int]{Cap: 8, Policy: DropOldest})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				r.Push(base*1000 + j)
			}
		}(i)
	}
	r.Close()
	wg.Wait()
	if got := r.Len(); got > r.Cap() {
		t.Fatalf("queue length %d exceeds capacity %d", got, r.Cap())
	}
	if r.Push(999999) {
		t.Fatal("push accepted after close")
	}
}
