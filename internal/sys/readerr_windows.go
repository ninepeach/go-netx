//go:build windows

package sys

import (
	"errors"
	"syscall"
)

// ClassifyRead classifies an error from WSARecvMsg on a UDP socket. Windows
// reports oversized datagrams and ICMP port-unreachable as read errors.
func ClassifyRead(err error) ReadErr {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return Fatal
	}
	return classifyWinsock(uint32(errno))
}
