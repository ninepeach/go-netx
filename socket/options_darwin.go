//go:build darwin

package socket

import "syscall"

// macOS accepts SO_REUSEPORT but does not balance load across the sockets:
// one of them receives everything. It is supported so the same
// configuration runs on a development Mac and a Linux server.
func applyOptions(fd uintptr, opts Options, _ bool) error {
	if opts.ReusePort {
		if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEPORT, 1); err != nil {
			return err
		}
	}
	return fastOpenUnsupported(opts)
}
