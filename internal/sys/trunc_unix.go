//go:build unix && !linux

package sys

import "syscall"

// MsgTrunc is the recvmsg flag reporting a datagram larger than the buffer.
const MsgTrunc = syscall.MSG_TRUNC
