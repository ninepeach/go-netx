//go:build unix

package socket

import (
	"syscall"
	"testing"
)

// Checks: SOCK-08
func TestFastOpenRunsWithoutWhenKernelLacksIt(t *testing.T) {
	for _, errno := range []syscall.Errno{syscall.ENOPROTOOPT, syscall.EOPNOTSUPP, syscall.ENOTSUP} {
		if err := fastOpenResult(errno); err != nil {
			t.Fatalf("%v: got %v, want nil (run without Fast Open)", errno, err)
		}
	}
	for _, errno := range []syscall.Errno{syscall.EINVAL, syscall.EPERM, syscall.EBADF} {
		if err := fastOpenResult(errno); err != errno {
			t.Fatalf("%v was swallowed or rewrapped: %v", errno, err)
		}
	}
	if fastOpenResult(nil) != nil {
		t.Fatal("nil error changed")
	}
}
