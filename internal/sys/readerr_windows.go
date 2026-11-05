//go:build windows

package sys

import (
	"errors"
	"syscall"
)

const (
	wsaeMsgSize   = 10040
	wsaeNetReset  = 10052
	wsaeConnReset = 10054
)

// ClassifyRead classifies an error from WSARecvMsg on a UDP socket. Windows
// reports oversized datagrams and ICMP port-unreachable as read errors.
func ClassifyRead(err error) ReadErr {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return Fatal
	}
	switch errno {
	case wsaeMsgSize:
		return Truncated
	case wsaeConnReset, wsaeNetReset:
		return Transient
	}
	return Fatal
}
