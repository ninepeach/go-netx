package udp

import (
	"errors"
	"net"
	"os"
	"syscall"
)

// OffloadSupport reports whether the kernel exposes UDP segmentation and
// receive-coalescing options on this socket. It does not enable them.
func OffloadSupport(c *net.UDPConn) (segmentation, coalescing bool, err error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return false, false, err
	}
	var inner error
	err = raw.Control(func(fd uintptr) {
		probe := func(option int) bool {
			_, err := syscall.GetsockoptInt(int(fd), syscall.IPPROTO_UDP, option)
			if err != nil && err != syscall.ENOPROTOOPT && err != syscall.EOPNOTSUPP {
				inner = errors.Join(inner, os.NewSyscallError("getsockopt", err))
			}
			return err == nil
		}
		segmentation = probe(103) // UDP_SEGMENT
		coalescing = probe(104)   // UDP_GRO
	})
	return segmentation, coalescing, errors.Join(err, inner)
}
