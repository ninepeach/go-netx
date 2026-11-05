//go:build !linux

package socket

import "runtime"

// fastOpenUnsupported reports unsupported TCP Fast Open without a fallback.
func fastOpenUnsupported(opts Options) error {
	if opts.FastOpen.Enabled {
		return &UnsupportedOptionError{Option: "TCP Fast Open", Platform: runtime.GOOS}
	}
	return nil
}
