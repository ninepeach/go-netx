//go:build unix

package sys

import (
	"errors"
	"net"
	"os"
	"syscall"
	"testing"
)

func TestClassifyRead(t *testing.T) {
	wrap := func(e syscall.Errno) error {
		return &net.OpError{Op: "read", Err: os.NewSyscallError("recvmsg", e)}
	}
	for _, tc := range []struct {
		err  error
		want ReadErr
	}{
		{wrap(syscall.ENOBUFS), Transient},
		{wrap(syscall.EINTR), Transient},
		{wrap(syscall.ECONNREFUSED), Transient},
		{wrap(syscall.EBADF), Fatal},
		{wrap(syscall.EINVAL), Fatal},
		{errors.New("not an errno"), Fatal},
	} {
		if got := ClassifyRead(tc.err); got != tc.want {
			t.Errorf("%v: got %d want %d", tc.err, got, tc.want)
		}
	}
}
