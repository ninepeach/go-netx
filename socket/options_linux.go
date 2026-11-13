package socket

import (
	"strings"
	"syscall"
)

const tcpFastOpen = 23 // Linux TCP_FASTOPEN

func bindInterface(fd int, _ string, name string) error {
	if strings.ContainsRune(name, 0) || len(name) >= syscall.IFNAMSIZ {
		return syscall.EINVAL
	}
	return syscall.BindToDevice(fd, name)
}

func setMark(fd int, mark uint32) error {
	return syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_MARK, int(mark))
}

func fastOpen(fd int, listen bool) error {
	const tcpFastOpenConnect = 30
	const queue = 128
	if listen {
		return syscall.SetsockoptInt(fd, syscall.IPPROTO_TCP, tcpFastOpen, queue)
	}
	return syscall.SetsockoptInt(fd, syscall.IPPROTO_TCP, tcpFastOpenConnect, 1)
}
