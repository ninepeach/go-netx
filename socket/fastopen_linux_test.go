//go:build linux

package socket

import (
	"syscall"
	"testing"
)

// Checks: SOCK-07
func TestFastOpenIgnoreOnlyUnsupportedErrors(t *testing.T) {
	for _, errno := range []syscall.Errno{syscall.ENOPROTOOPT, syscall.EOPNOTSUPP} {
		if err := fastOpenResult(errno, UnsupportedIgnore); err != nil {
			t.Fatalf("%v ignored policy: got %v", errno, err)
		}
		if err := fastOpenResult(errno, UnsupportedError); !IsUnsupported(err) {
			t.Fatalf("%v error policy: got %v, want UnsupportedOptionError", errno, err)
		}
	}
	for _, errno := range []syscall.Errno{syscall.EINVAL, syscall.EPERM, syscall.EBADF} {
		for _, policy := range []UnsupportedPolicy{UnsupportedIgnore, UnsupportedError} {
			err := fastOpenResult(errno, policy)
			if err != errno {
				t.Fatalf("%v (policy %d) was swallowed or rewrapped: %v", errno, policy, err)
			}
		}
	}
	if fastOpenResult(nil, UnsupportedError) != nil {
		t.Fatal("nil error changed")
	}
}
