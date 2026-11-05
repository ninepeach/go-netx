//go:build unix

package socket

import "syscall"

func fdBufferOps(fd uintptr) bufferOps {
	return bufferOps{
		set: func(opt, v int) error { return syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, opt, v) },
		get: func(opt int) (int, error) { return syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, opt) },
	}
}

func applyBuffers(fd uintptr, opts Options) error {
	ops := fdBufferOps(fd)
	if opts.RecvBuffer > 0 {
		if err := ensureBuffer(ops, recvBufferKind, opts.RecvBuffer, kernelDoublesBuffers); err != nil {
			return err
		}
	}
	if opts.SendBuffer > 0 {
		if err := ensureBuffer(ops, sendBufferKind, opts.SendBuffer, kernelDoublesBuffers); err != nil {
			return err
		}
	}
	return nil
}
