package socket

import (
	"errors"
	"fmt"
	"net"
	"syscall"
)

// UnsupportedPolicy controls what happens when an optional socket feature is
// unavailable on the current platform.
type UnsupportedPolicy uint8

const (
	UnsupportedError UnsupportedPolicy = iota
	UnsupportedIgnore
)

// FastOpenOptions controls TCP Fast Open for listeners and outbound sockets.
type FastOpenOptions struct {
	Enabled     bool
	Backlog     int
	Unsupported UnsupportedPolicy
}

// Options contains portable socket configuration. Platform-specific features
// follow the policy configured by the caller.
type Options struct {
	FastOpen  FastOpenOptions
	ReusePort bool
	// RecvBuffer and SendBuffer set SO_RCVBUF and SO_SNDBUF in bytes when
	// positive, before bind. The kernel may round or cap them. On a TCP
	// listener they are inherited by every accepted connection, and setting
	// them before listen lets TCP window scaling take them into account.
	RecvBuffer int
	SendBuffer int
}

// BufferSetter is implemented by *net.UDPConn and *net.TCPConn.
type BufferSetter interface {
	SetReadBuffer(int) error
	SetWriteBuffer(int) error
}

// SetBuffers applies RecvBuffer and SendBuffer to an existing connection,
// for sockets not created through ListenConfig or Dialer.
func SetBuffers(c BufferSetter, opts Options) error {
	if opts.RecvBuffer > 0 {
		if err := c.SetReadBuffer(opts.RecvBuffer); err != nil {
			return err
		}
	}
	if opts.SendBuffer > 0 {
		if err := c.SetWriteBuffer(opts.SendBuffer); err != nil {
			return err
		}
	}
	return nil
}

// UnsupportedError reports that a requested option is unavailable.
type UnsupportedOptionError struct {
	Option   string
	Platform string
}

func (e *UnsupportedOptionError) Error() string {
	return fmt.Sprintf("socket: %s is unsupported on %s", e.Option, e.Platform)
}

// IsUnsupported reports whether err represents an unavailable socket option.
func IsUnsupported(err error) bool {
	var target *UnsupportedOptionError
	return errors.As(err, &target)
}

// ListenConfig returns a net.ListenConfig configured with the requested
// pre-bind socket options.
func ListenConfig(opts Options) net.ListenConfig {
	return net.ListenConfig{Control: control(opts, false)}
}

// Dialer applies outbound socket options to base. The returned value is a copy;
// the supplied dialer is not mutated.
func Dialer(base net.Dialer, opts Options) net.Dialer {
	base.Control = control(opts, true)
	return base
}

func control(opts Options, outbound bool) func(string, string, syscall.RawConn) error {
	return func(network, _ string, raw syscall.RawConn) error {
		isTCP := network == "tcp" || network == "tcp4" || network == "tcp6"
		isUDP := network == "udp" || network == "udp4" || network == "udp6"
		if !isTCP && !isUDP {
			return nil
		}
		options := opts
		if !isTCP {
			options.FastOpen = FastOpenOptions{}
		}
		var applyErr error
		if err := raw.Control(func(fd uintptr) {
			if applyErr = applyBuffers(fd, options); applyErr == nil {
				applyErr = applyOptions(fd, options, outbound)
			}
		}); err != nil {
			return err
		}
		return applyErr
	}
}
