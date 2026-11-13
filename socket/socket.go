// Package socket configures sockets through net's control hooks.
package socket

import (
	"context"
	"errors"
	"fmt"
	"syscall"
)

// Options specifies the socket options to apply. A nil Mark leaves the
// mark alone, while a pointer to zero explicitly clears it.
type Options struct {
	Interface           string
	Mark                *uint32
	FastOpen, ReusePort bool
}

// DialControl returns a hook for net.Dialer.ControlContext.
func DialControl(o Options) func(context.Context, string, string, syscall.RawConn) error {
	hook := control(o, false)
	return func(_ context.Context, network, address string, c syscall.RawConn) error {
		return hook(network, address, c)
	}
}

// ListenControl returns a hook for net.ListenConfig.Control.
func ListenControl(o Options) func(string, string, syscall.RawConn) error {
	return control(o, true)
}

func control(o Options, listen bool) func(string, string, syscall.RawConn) error {
	if o.Mark != nil {
		mark := *o.Mark
		o.Mark = &mark
	}
	return func(network, _ string, c syscall.RawConn) error {
		if o == (Options{}) {
			return nil
		}
		switch network {
		case "tcp4", "tcp6":
		case "udp4", "udp6":
			if o.FastOpen {
				return optionError("FastOpen", nil)
			}
		default:
			return fmt.Errorf("socket: network %q: %w", network, errors.ErrUnsupported)
		}
		var optionErr error
		if err := c.Control(func(fd uintptr) {
			optionErr = apply(fd, network, o, listen)
		}); err != nil {
			return err
		}
		return optionErr
	}
}

// optionError names the option that failed. It wraps errors.ErrUnsupported
// only when the platform or the kernel does not have the option; a refused
// or invalid request (no permission, no such interface) keeps its cause and
// is not reported as unsupported.
func optionError(name string, cause error) error {
	switch {
	case cause == nil:
		return fmt.Errorf("socket: %s: %w", name, errors.ErrUnsupported)
	case errors.Is(cause, errors.ErrUnsupported):
		return fmt.Errorf("socket: %s: %w", name, cause)
	case errors.Is(cause, syscall.ENOPROTOOPT):
		return fmt.Errorf("socket: %s: %w: %w", name, errors.ErrUnsupported, cause)
	}
	return fmt.Errorf("socket: %s: %w", name, cause)
}
