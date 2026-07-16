//go:build !linux

package socket

import "runtime"

func applyOptions(_ uintptr, opts Options, _ bool) error {
	if opts.FastOpen.Enabled && opts.FastOpen.Unsupported == UnsupportedError {
		return &UnsupportedOptionError{Option: "TCP Fast Open", Platform: runtime.GOOS}
	}
	if opts.ReusePort {
		return &UnsupportedOptionError{Option: "SO_REUSEPORT", Platform: runtime.GOOS}
	}
	return nil
}
