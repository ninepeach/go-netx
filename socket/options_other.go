//go:build !linux && !darwin && !freebsd

package socket

import "runtime"

// Fast Open is not implemented here; sockets run without it (D-18).
var platformCapabilities = Capabilities{}

func applyOptions(_ uintptr, opts Options, _ bool) error {
	// ReusePort changes behaviour (a second bind fails without it), so it
	// is an error rather than a silent no-op.
	if opts.ReusePort {
		return &UnsupportedOptionError{Option: "SO_REUSEPORT", Platform: runtime.GOOS}
	}
	return nil
}
