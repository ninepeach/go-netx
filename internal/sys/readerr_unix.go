//go:build unix

package sys

import (
	"errors"
	"syscall"
)

// ClassifyRead classifies an error from recvmsg on a UDP socket.
func ClassifyRead(err error) ReadErr {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return Fatal
	}
	switch errno {
	case syscall.EINTR, syscall.EAGAIN, syscall.ENOBUFS, syscall.ENOMEM,
		syscall.ECONNREFUSED, syscall.EHOSTUNREACH, syscall.ENETUNREACH:
		// Resource pressure, or an ICMP error queued on the socket.
		return Transient
	}
	return Fatal
}
