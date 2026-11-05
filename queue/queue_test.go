package queue

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Checks: QUEUE-03, QUEUE-15
func TestRejectKeepsOwnership(t *testing.T) {
	r := New(Options[int]{Cap: 2})
	if !r.Push(1) || !r.Push(2) || r.Push(3) {
		t.Fatal("reject policy")
	}
	if s := r.Stats(); s.Rejected != 1 || s.Len != 2 {
		t.Fatalf("%+v", s)
	}
}

// Checks: QUEUE-04
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

// Checks: QUEUE-02
func TestCapRoundsUp(t *testing.T) {
	if c := New(Options[int]{Cap: 5}).Cap(); c != 8 {
		t.Fatalf("cap=%d", c)
	}
}

// Checks: QUEUE-10
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

// Checks: QUEUE-11
func TestPushWaitContext(t *testing.T) {
	r := New(Options[int]{Cap: 1})
	r.Push(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := r.PushWait(ctx, 2); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

// Checks: QUEUE-06
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

// Checks: QUEUE-08
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

// Checks: QUEUE-12
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

// Checks: NF-06, QUEUE-13
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

// Checks: NF-15, QUEUE-16
func BenchmarkPushPop(b *testing.B) {
	r := New(Options[int]{Cap: 1024})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r.Push(i)
		r.Pop()
	}
}

// Checks: QUEUE-09
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

// Checks: QUEUE-07, QUEUE-17
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

// Checks: QUEUE-01
func TestFIFOOrder(t *testing.T) {
	r := New(Options[int]{Cap: 8})
	for round := 0; round < 3; round++ { // wraps the ring several times
		for i := 0; i < 8; i++ {
			if !r.Push(round*8 + i) {
				t.Fatal("push refused below capacity")
			}
		}
		for i := 0; i < 8; i++ {
			if v, ok := r.Pop(); !ok || v != round*8+i {
				t.Fatalf("round %d: pop %d = %d, %v", round, i, v, ok)
			}
		}
	}
	if _, ok := r.Pop(); ok {
		t.Fatal("pop from empty queue")
	}
}

// Checks: QUEUE-05
func TestNewRejectsNonPositiveCap(t *testing.T) {
	for _, c := range []int{0, -1} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("Cap %d accepted", c)
				}
			}()
			New(Options[int]{Cap: c})
		}()
	}
}

// Checks: QUEUE-07
func TestRepeatedClose(t *testing.T) {
	r := New(Options[int]{Cap: 2})
	r.Push(1)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r.Close() }()
	}
	wg.Wait()
	r.Close()
	if v, err := r.PopWait(context.Background()); err != nil || v != 1 {
		t.Fatal("item lost by repeated Close")
	}
	if _, err := r.PopWait(context.Background()); err != ErrClosed {
		t.Fatal(err)
	}
}

// Checks: QUEUE-11
func TestPopWaitContext(t *testing.T) {
	r := New(Options[int]{Cap: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := r.PopWait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

// Checks: QUEUE-14, NF-06
func TestMultiConsumerNoLossNoDuplicate(t *testing.T) {
	r := New(Options[int]{Cap: 16})
	const producers, consumers, each = 4, 4, 2000
	var pwg sync.WaitGroup
	for p := 0; p < producers; p++ {
		pwg.Add(1)
		go func(p int) {
			defer pwg.Done()
			for i := 0; i < each; i++ {
				if err := r.PushWait(context.Background(), p*each+i); err != nil {
					t.Error(err)
					return
				}
			}
		}(p)
	}
	seen := make([]atomic.Int32, producers*each)
	var cwg sync.WaitGroup
	for c := 0; c < consumers; c++ {
		cwg.Add(1)
		go func() {
			defer cwg.Done()
			for {
				v, err := r.PopWait(context.Background())
				if err != nil {
					return
				}
				seen[v].Add(1)
			}
		}()
	}
	pwg.Wait()
	r.Close()
	cwg.Wait()
	for v := range seen {
		if n := seen[v].Load(); n != 1 {
			t.Fatalf("item %d delivered %d times", v, n)
		}
	}
}

// Checks: QUEUE-15
func TestStatsCountEveryOutcome(t *testing.T) {
	var dropped []int
	r := New(Options[int]{Cap: 2, Policy: DropOldest, OnDrop: func(v int) { dropped = append(dropped, v) }})
	for i := 0; i < 5; i++ {
		r.Push(i) // 2 kept, 3 evicted
	}
	r.Pop()
	r.Close()
	r.Push(9) // rejected after close
	r.Drain() // 1 left, dropped
	want := Stats{Len: 0, Pushed: 5, Popped: 1, Dropped: 4, Rejected: 1}
	if got := r.Stats(); got != want {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if len(dropped) != 4 {
		t.Fatalf("OnDrop saw %v", dropped)
	}

	rej := New(Options[int]{Cap: 1})
	rej.Push(1)
	rej.Push(2)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	rej.PushWait(ctx, 3) // times out: not counted as anything
	if got := rej.Stats(); got != (Stats{Len: 1, Pushed: 1, Rejected: 1}) {
		t.Fatalf("reject queue: %+v", got)
	}
}
