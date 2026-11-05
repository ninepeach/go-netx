//go:build linux

package sys

import (
	"syscall"
	"testing"
	"unsafe"
)

func cmsg(level, typ int32, data []byte) []byte {
	hdr := int(unsafe.Sizeof(syscall.Cmsghdr{}))
	b := make([]byte, cmsgAlign(hdr+len(data)))
	h := (*syscall.Cmsghdr)(unsafe.Pointer(&b[0]))
	h.Level, h.Type = level, typ
	h.SetLen(hdr + len(data))
	copy(b[hdr:], data)
	return b
}

// Checks: SYS-03, UDP-30
func TestDropCountParse(t *testing.T) {
	other := cmsg(syscall.SOL_SOCKET, syscall.SCM_TIMESTAMP, make([]byte, 16))
	drops := cmsg(syscall.SOL_SOCKET, soRxqOvfl, []byte{7, 0, 0, 0})
	if n, ok := DropCount(append(other, drops...)); !ok || n != 7 {
		t.Fatalf("n=%d ok=%v", n, ok)
	}
	if _, ok := DropCount(other); ok {
		t.Fatal("found drops in unrelated cmsg")
	}
	if _, ok := DropCount([]byte{1, 2, 3}); ok {
		t.Fatal("parsed truncated buffer")
	}
	bad := cmsg(syscall.SOL_SOCKET, soRxqOvfl, []byte{1, 0, 0, 0})
	(*syscall.Cmsghdr)(unsafe.Pointer(&bad[0])).SetLen(1 << 20) // lies about length
	if _, ok := DropCount(bad); ok {
		t.Fatal("trusted oversized length")
	}
}

// Checks: SYS-03
func TestDropCountDoesNotAllocate(t *testing.T) {
	other := cmsg(syscall.SOL_SOCKET, syscall.SCM_TIMESTAMP, make([]byte, 16))
	buf := append(other, cmsg(syscall.SOL_SOCKET, soRxqOvfl, []byte{9, 0, 0, 0})...)
	allocs := testing.AllocsPerRun(1000, func() {
		if n, ok := DropCount(buf); !ok || n != 9 {
			t.Fatal("parse failed")
		}
	})
	if allocs != 0 {
		t.Fatalf("%.1f allocs per parse", allocs)
	}
}
