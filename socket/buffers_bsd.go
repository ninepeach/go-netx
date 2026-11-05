//go:build unix && !linux

package socket

import "syscall"

const kernelDoublesBuffers = false

var (
	recvBufferKind = bufferKind{name: "receive buffer", opt: syscall.SO_RCVBUF}
	sendBufferKind = bufferKind{name: "send buffer", opt: syscall.SO_SNDBUF}
)

func bufferHint(string) string { return "raise kern.ipc.maxsockbuf" }
