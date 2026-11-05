// Package leak checks that tests give back the goroutines and file
// descriptors they used. It replaces an external dependency such as goleak
// so go-netx stays dependency-free.
package leak

import (
	"fmt"
	"runtime"
	"time"
)

// Snapshot is a baseline of goroutines and open file descriptors.
type Snapshot struct {
	Goroutines int
	FDs        int // -1 where the platform cannot count them
}

// Take records the current baseline. Call it before the code under test
// starts anything.
func Take() Snapshot {
	return Snapshot{Goroutines: runtime.NumGoroutine(), FDs: openFDs()}
}

// Check waits up to timeout for goroutines and file descriptors to fall
// back to the baseline plus slack, and returns an error describing any
// excess, with a goroutine dump to show what is still running.
func (s Snapshot) Check(timeout time.Duration, slack int) error {
	deadline := time.Now().Add(timeout)
	for {
		now := Take()
		gOK := now.Goroutines <= s.Goroutines+slack
		fOK := s.FDs < 0 || now.FDs < 0 || now.FDs <= s.FDs+slack
		if gOK && fOK {
			return nil
		}
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			n := runtime.Stack(buf, true)
			return fmt.Errorf("leak: goroutines %d→%d, fds %d→%d (slack %d)\n%s",
				s.Goroutines, now.Goroutines, s.FDs, now.FDs, slack, buf[:n])
		}
		runtime.GC() // lets finalizers close dropped sockets
		time.Sleep(5 * time.Millisecond)
	}
}
