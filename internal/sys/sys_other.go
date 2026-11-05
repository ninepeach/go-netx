//go:build !linux

package sys

import "syscall"

// OOBSize is zero where kernel drop counts are unavailable.
const OOBSize = 0

// EnableDropCount is unsupported on this platform.
func EnableDropCount(syscall.RawConn) bool { return false }

// DropCount is unsupported on this platform.
func DropCount([]byte) (uint32, bool) { return 0, false }
