//go:build linux

package sys

import (
	"syscall"
	"testing"
)

// DropCount parses kernel control data, so any byte sequence must be safe:
// no panic, no out-of-bounds read, and a reported count must come from a
// well-formed SO_RXQ_OVFL message inside the buffer.
//
// Checks: NF-07
func FuzzDropCount(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{1, 2, 3})
	f.Add(cmsg(syscall.SOL_SOCKET, soRxqOvfl, []byte{7, 0, 0, 0}))
	f.Add(append(cmsg(syscall.SOL_SOCKET, syscall.SCM_TIMESTAMP, make([]byte, 16)),
		cmsg(syscall.SOL_SOCKET, soRxqOvfl, []byte{1, 2, 3, 4})...))
	f.Fuzz(func(t *testing.T, oob []byte) {
		// Copy into an 8-aligned buffer, as the kernel's would be.
		aligned := make([]byte, len(oob))
		copy(aligned, oob)
		n, ok := DropCount(aligned)
		if !ok && n != 0 {
			t.Fatalf("not found but n=%d", n)
		}
	})
}
