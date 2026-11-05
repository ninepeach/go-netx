//go:build !linux && !darwin && !freebsd

package socket

import "runtime"

func applyOptions(_ uintptr, opts Options, _ bool) error {
	if err := fastOpenUnsupported(opts); err != nil {
		return err
	}
	if opts.ReusePort {
		return &UnsupportedOptionError{Option: "SO_REUSEPORT", Platform: runtime.GOOS}
	}
	return nil
}
