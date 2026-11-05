//go:build linux

package sys

import (
	"syscall"
	"unsafe"
)

// MsgTrunc is the recvmsg flag reporting a datagram larger than the buffer.
const MsgTrunc = syscall.MSG_TRUNC

// OOBSize is the control buffer size needed for EnableDropCount.
const OOBSize = 64

const soRxqOvfl = 40 // SO_RXQ_OVFL

// EnableDropCount asks the kernel to report the socket's cumulative receive
// queue overflow count with every datagram.
func EnableDropCount(rc syscall.RawConn) bool {
	ok := false
	_ = rc.Control(func(fd uintptr) {
		ok = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, soRxqOvfl, 1) == nil
	})
	return ok
}

// DropCount extracts the cumulative kernel drop count from control data.
// It does not allocate.
func DropCount(oob []byte) (uint32, bool) {
	const hdr = int(unsafe.Sizeof(syscall.Cmsghdr{}))
	for len(oob) >= hdr {
		h := (*syscall.Cmsghdr)(unsafe.Pointer(&oob[0]))
		l := int(h.Len)
		if l < hdr || l > len(oob) {
			return 0, false
		}
		if h.Level == syscall.SOL_SOCKET && h.Type == soRxqOvfl && l >= hdr+4 {
			return *(*uint32)(unsafe.Pointer(&oob[hdr])), true
		}
		next := cmsgAlign(l)
		if next > len(oob) {
			return 0, false
		}
		oob = oob[next:]
	}
	return 0, false
}

func cmsgAlign(n int) int {
	const a = int(unsafe.Sizeof(uintptr(0)))
	return (n + a - 1) &^ (a - 1)
}
