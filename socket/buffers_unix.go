//go:build unix

package socket

import "syscall"

func applyBuffers(fd uintptr, opts Options) error {
	if opts.RecvBuffer > 0 {
		if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF, opts.RecvBuffer); err != nil {
			return err
		}
	}
	if opts.SendBuffer > 0 {
		if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_SNDBUF, opts.SendBuffer); err != nil {
			return err
		}
	}
	return nil
}
