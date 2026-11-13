//go:build linux || darwin

package udp

import (
	"net"
	"os"
	"syscall"
)

func waitPacket(c *net.UDPConn) error {
	raw, err := c.SyscallConn()
	if err != nil {
		return err
	}
	var inner error
	err = raw.Read(func(fd uintptr) bool {
		var p [1]byte
		for {
			_, _, inner = syscall.Recvfrom(int(fd), p[:], syscall.MSG_PEEK)
			if inner != syscall.EINTR {
				return inner != syscall.EAGAIN && inner != syscall.EWOULDBLOCK
			}
		}
	})
	if err != nil {
		return err
	}
	if inner != nil {
		return os.NewSyscallError("recvfrom", inner)
	}
	return nil
}
