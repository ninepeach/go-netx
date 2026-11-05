//go:build !unix && !windows

package socket

import "runtime"

func bufferHint(string) string { return "" }

func applyBuffers(_ uintptr, opts Options) error {
	if opts.RecvBuffer > 0 || opts.SendBuffer > 0 {
		return &UnsupportedOptionError{Option: "socket buffer sizes", Platform: runtime.GOOS}
	}
	return nil
}
