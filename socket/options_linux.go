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
	return fastOpenResult(syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, option, value))
}

// fastOpenResult converts only kernel errors that mean TCP Fast Open is
// unavailable. All other system-call errors are preserved.
func fastOpenResult(err error) error {
	if err == nil {
		return nil
	}
	if !errors.Is(err, syscall.ENOPROTOOPT) && !errors.Is(err, syscall.EOPNOTSUPP) {
		return err
	}
	return &UnsupportedOptionError{Option: "TCP Fast Open", Platform: "linux"}
}
