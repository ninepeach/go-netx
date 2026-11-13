//go:build linux || darwin

package socket

import (
	"errors"
	"syscall"
)

func apply(fd uintptr, network string, o Options, listen bool) error {
	if o.Interface != "" {
		if err := bindInterface(int(fd), network, o.Interface); err != nil {
			return optionError("Interface", err)
		}
	}
	if o.Mark != nil {
		if err := setMark(int(fd), *o.Mark); err != nil {
			return optionError("Mark", err)
		}
	}
	if o.FastOpen {
		err := fastOpen(int(fd), listen)
		if err != nil && !errors.Is(err, syscall.ENOPROTOOPT) && !errors.Is(err, syscall.EOPNOTSUPP) {
			return optionError("FastOpen", err)
		}
	}
	if o.ReusePort {
		if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, soReusePort, 1); err != nil {
			return optionError("ReusePort", err)
		}
	}
	return nil
}
