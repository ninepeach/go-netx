package socket

import "syscall"

// Linux doubles SO_RCVBUF and SO_SNDBUF and caps them at rmem_max and
// wmem_max; the FORCE variants bypass the cap for CAP_NET_ADMIN.
const kernelDoublesBuffers = true

var (
	recvBufferKind = bufferKind{name: "receive buffer", opt: syscall.SO_RCVBUF, force: syscall.SO_RCVBUFFORCE}
	sendBufferKind = bufferKind{name: "send buffer", opt: syscall.SO_SNDBUF, force: syscall.SO_SNDBUFFORCE}
)

func bufferHint(option string) string {
	if option == "send buffer" {
		return "raise net.core.wmem_max, or run with CAP_NET_ADMIN"
	}
	return "raise net.core.rmem_max, or run with CAP_NET_ADMIN"
}
