//go:build unix

package socket

import (
	"errors"
	"syscall"
)

// fastOpenResult treats kernel errors that mean "TCP Fast Open is not
// available" as success: Fast Open only saves a round trip, so the socket
// works without it (D-18). Every other error is returned unchanged.
func fastOpenResult(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.ENOPROTOOPT) || errors.Is(err, syscall.EOPNOTSUPP) || errors.Is(err, syscall.ENOTSUP) {
		return nil
	}
	return err
}
