package udp

import (
	"net"
	"syscall"
	"testing"
)

func TestWindowsTruncationError(t *testing.T) {
	if !oversized(&net.OpError{Op: "read", Err: syscall.Errno(10040)}) || oversized(net.ErrClosed) {
		t.Fatal("incorrect Winsock truncation classification")
	}
}
