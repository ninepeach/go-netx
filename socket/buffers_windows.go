//go:build windows

package socket

import "syscall"

const kernelDoublesBuffers = false

var (
	recvBufferKind = bufferKind{name: "receive buffer", opt: syscall.SO_RCVBUF}
	sendBufferKind = bufferKind{name: "send buffer", opt: syscall.SO_SNDBUF}
)

func bufferHint(string) string { return "" }

func applyBuffers(fd uintptr, opts Options) error {
	h := syscall.Handle(fd)
	ops := bufferOps{
		set: func(opt, v int) error { return syscall.SetsockoptInt(h, syscall.SOL_SOCKET, opt, v) },
		get: func(opt int) (int, error) { return syscall.GetsockoptInt(h, syscall.SOL_SOCKET, opt) },
	}
	if opts.RecvBuffer > 0 {
		if err := ensureBuffer(ops, recvBufferKind, opts.RecvBuffer, false); err != nil {
			return err
		}
	}
	if opts.SendBuffer > 0 {
		if err := ensureBuffer(ops, sendBufferKind, opts.SendBuffer, false); err != nil {
			return err
		}
	}
	return nil
}
