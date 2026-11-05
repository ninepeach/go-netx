package socket

import (
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
)

// FastOpenOptions controls TCP Fast Open for listeners and outbound sockets.
// If the platform or kernel does not support it, socket creation fails.
type FastOpenOptions struct {
	Enabled bool
	Backlog int
}

// Options contains portable socket configuration. Platform-specific features
// return explicit errors when a requested feature is unavailable.
type Options struct {
	FastOpen  FastOpenOptions
	ReusePort bool
	// RecvBuffer and SendBuffer set SO_RCVBUF and SO_SNDBUF in bytes when
	// positive, before bind. The kernel may round or cap them. On a TCP
	// listener they are inherited by every accepted connection, and setting
	// them before listen lets TCP window scaling take them into account.
	RecvBuffer int
	SendBuffer int
	// KeepAlive configures TCP keep-alive probes. On a listener it applies
	// to every accepted connection; on a Dialer to every dialed one. The
	// zero value keeps Go's default (enabled, 15s).
	KeepAlive KeepAlive
}

// KeepAlive configures TCP keep-alive probes, which let a server notice a
// peer that vanished without closing the connection (for example a phone
// that switched networks).
type KeepAlive struct {
	// Disable turns keep-alive probes off. The other fields are ignored.
	Disable bool
	// Idle is how long a connection must be idle before the first probe.
	Idle time.Duration
	// Interval is the time between probes.
	Interval time.Duration
	// Count is how many unanswered probes close the connection.
	Count int
}

// IsZero reports whether k leaves Go's default behaviour in place.
func (k KeepAlive) IsZero() bool { return k == KeepAlive{} }

// apply returns the net package's keep-alive duration and configuration.
// Zero fields in an enabled configuration fall back to Go's defaults.
func (k KeepAlive) apply() (time.Duration, net.KeepAliveConfig) {
	switch {
	case k.Disable:
		return -1, net.KeepAliveConfig{}
	case k.IsZero():
		return 0, net.KeepAliveConfig{}
	}
	return 0, net.KeepAliveConfig{Enable: true, Idle: k.Idle, Interval: k.Interval, Count: k.Count}
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
	keepAlive, keepAliveConfig := opts.KeepAlive.apply()
	return net.ListenConfig{Control: control(opts, false), KeepAlive: keepAlive, KeepAliveConfig: keepAliveConfig}
}

// Dialer applies outbound socket options to base. The returned value is a copy;
// the supplied dialer is not mutated.
func Dialer(base net.Dialer, opts Options) net.Dialer {
	base.Control = control(opts, true)
	if !opts.KeepAlive.IsZero() {
		base.KeepAlive, base.KeepAliveConfig = opts.KeepAlive.apply()
	}
	return base
}

func control(opts Options, outbound bool) func(string, string, syscall.RawConn) error {
	return func(network, _ string, raw syscall.RawConn) error {
		isTCP := network == "tcp" || network == "tcp4" || network == "tcp6"
		isUDP := network == "udp" || network == "udp4" || network == "udp6"
		if opts.FastOpen.Enabled && !isTCP {
			return fmt.Errorf("socket: TCP Fast Open requires TCP, got network %q", network)
		}
		if opts.ReusePort && !isTCP && !isUDP {
			return fmt.Errorf("socket: SO_REUSEPORT requires an IP socket, got network %q", network)
		}
		var applyErr error
		if err := raw.Control(func(fd uintptr) {
			if applyErr = applyBuffers(fd, opts); applyErr == nil && (isTCP || isUDP) {
				applyErr = applyOptions(fd, opts, outbound)
			}
		}); err != nil {
			return err
		}
		return applyErr
	}
}
