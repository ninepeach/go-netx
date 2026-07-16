//go:build linux

package socket

import (
	"runtime"
	"syscall"
)

const (
	tcpFastOpen        = 23
	tcpFastOpenConnect = 30
	soReusePort        = 15
)

func applyOptions(fd uintptr, opts Options, outbound bool) error {
	if opts.ReusePort {
		if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, soReusePort, 1); err != nil {
			return err
		}
	}
	if !opts.FastOpen.Enabled {
		return nil
	}
	value, option := opts.FastOpen.Backlog, tcpFastOpen
	if value <= 0 {
		value = 128
	}
	if outbound {
		value, option = 1, tcpFastOpenConnect
	}
	if err := syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, option, value); err != nil {
		if opts.FastOpen.Unsupported == UnsupportedIgnore {
			return nil
		}
		return err
	}
	return nil
}

var _ = runtime.GOOS
