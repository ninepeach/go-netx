package udp

import (
	"errors"
	"syscall"
)

func oversized(err error) bool {
	const wsaMsgSize = syscall.Errno(10040)
	return errors.Is(err, wsaMsgSize)
}
