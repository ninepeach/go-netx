//go:build linux

package socket

import (
	"syscall"
	"testing"
)

// Checks: SOCK-08
func TestFastOpenAlwaysReportsUnsupportedKernelErrors(t *testing.T) {
	for _, errno := range []syscall.Errno{syscall.ENOPROTOOPT, syscall.EOPNOTSUPP} {
		if err := fastOpenResult(errno); !IsUnsupported(err) {
			t.Fatalf("%v: got %v, want UnsupportedOptionError", errno, err)
		}
	}
	for _, errno := range []syscall.Errno{syscall.EINVAL, syscall.EPERM, syscall.EBADF} {
		err := fastOpenResult(errno)
		if err != errno {
			t.Fatalf("%v was swallowed or rewrapped: %v", errno, err)
		}
	}
	if fastOpenResult(nil) != nil {
		t.Fatal("nil error changed")
	}
}
