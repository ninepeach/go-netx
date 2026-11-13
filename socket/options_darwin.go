package socket

import (
	"errors"
	"net"
	"syscall"
)

func bindInterface(fd int, network, name string) error {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return err
	}
	if network == "tcp6" || network == "udp6" {
		return syscall.SetsockoptInt(fd, syscall.IPPROTO_IPV6, syscall.IPV6_BOUND_IF, iface.Index)
	}
	return syscall.SetsockoptInt(fd, syscall.IPPROTO_IP, syscall.IP_BOUND_IF, iface.Index)
}

func setMark(int, uint32) error { return errors.ErrUnsupported }

func fastOpen(fd int, listen bool) error {
	const tcpFastOpen = 0x105 // XNU netinet/tcp.h
	if listen {
		err := syscall.SetsockoptInt(fd, syscall.IPPROTO_TCP, tcpFastOpen, 1)
		// Some XNU versions only accept this option after listen(2).
		// ListenConfig.Control runs before the socket is bound.
		if errors.Is(err, syscall.EINVAL) {
			return nil
		}
		return err
	}
	return nil
}
