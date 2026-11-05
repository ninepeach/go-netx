//go:build darwin

package socket

import (
	"runtime"
	"syscall"
)

func applyOptions(fd uintptr, opts Options, _ bool) error {
	if opts.ReusePort {
		if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEPORT, 1); err != nil {
			return err
		}
	}
	if opts.FastOpen.Enabled {
		return &UnsupportedOptionError{Option: "TCP Fast Open", Platform: runtime.GOOS}
	}
	return nil
}
