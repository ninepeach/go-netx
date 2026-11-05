//go:build !linux

package socket

import "runtime"

// fastOpenUnsupported applies the Unsupported policy where TCP Fast Open is
// not implemented.
func fastOpenUnsupported(opts Options) error {
	if opts.FastOpen.Enabled && opts.FastOpen.Unsupported == UnsupportedError {
		return &UnsupportedOptionError{Option: "TCP Fast Open", Platform: runtime.GOOS}
	}
	return nil
}
