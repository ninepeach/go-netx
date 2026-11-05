//go:build linux

package socket

import (
	"errors"
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
	return fastOpenResult(syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, option, value), opts.FastOpen.Unsupported)
}

// fastOpenResult applies the Unsupported policy only to errors meaning the
// kernel lacks TCP Fast Open. Any other error is returned unchanged, even
// under UnsupportedIgnore.
func fastOpenResult(err error, policy UnsupportedPolicy) error {
	if err == nil {
		return nil
	}
	if !errors.Is(err, syscall.ENOPROTOOPT) && !errors.Is(err, syscall.EOPNOTSUPP) {
		return err
	}
	if policy == UnsupportedIgnore {
		return nil
	}
	return &UnsupportedOptionError{Option: "TCP Fast Open", Platform: "linux"}
}
