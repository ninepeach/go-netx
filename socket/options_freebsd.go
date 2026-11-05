//go:build freebsd

package socket

import "syscall"

// soReusePortLB is SO_REUSEPORT_LB (FreeBSD 12+), which balances incoming
// connections and datagrams across the sockets like Linux SO_REUSEPORT.
// The syscall package does not define it.
const soReusePortLB = 0x00010000

func applyOptions(fd uintptr, opts Options, _ bool) error {
	if opts.ReusePort {
		if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, soReusePortLB, 1); err != nil {
			return err
		}
	}
	return fastOpenUnsupported(opts)
}
