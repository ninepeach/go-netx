package socket

import (
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
)

// FastOpenOptions controls TCP Fast Open for listeners and outbound sockets.
// Fast Open only saves a round trip, so where the platform or kernel lacks
// it the socket is created without it (see Supported); any other error
// from enabling it is returned. Backlog applies on Linux only.
type FastOpenOptions struct {
	Enabled bool
	Backlog int
}

// Options contains portable socket configuration. One Options value works
// on every supported platform: performance options the platform lacks run
// without the optimisation (Supported reports which are implemented),
// while options that change behaviour fail with UnsupportedOptionError.
type Options struct {
	FastOpen FastOpenOptions
	// ReusePort lets several sockets bind the same address. Linux and
	// FreeBSD (SO_REUSEPORT_LB) spread incoming connections and datagrams
	// across them; macOS allows the binding but does not spread load, so
	// one socket receives everything. Other platforms return an
	// UnsupportedOptionError.
	ReusePort bool
	// RecvBuffer and SendBuffer set SO_RCVBUF and SO_SNDBUF in bytes when
	// positive, before bind. The size is read back: when the kernel grants
	// less (Linux caps it at net.core.rmem_max and wmem_max without
	// failing), Linux retries with SO_RCVBUFFORCE or SO_SNDBUFFORCE, which
	// needs CAP_NET_ADMIN, and if the size is still short the socket is
	// not created and a *BufferSizeError is returned. On a TCP listener
	// they are inherited by every accepted connection, and setting them
	// before listen lets TCP window scaling take them into account.
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
// for sockets not created through ListenConfig or Dialer. When c also
// implements syscall.Conn (as *net.UDPConn and *net.TCPConn do), the sizes
// are verified as in Options and a short size is a *BufferSizeError;
// otherwise they are only set.
func SetBuffers(c BufferSetter, opts Options) error {
	if sc, ok := c.(syscall.Conn); ok && (opts.RecvBuffer > 0 || opts.SendBuffer > 0) {
		raw, err := sc.SyscallConn()
		if err != nil {
			return err
		}
		var applyErr error
		if err := raw.Control(func(fd uintptr) {
			applyErr = applyBuffers(fd, Options{RecvBuffer: opts.RecvBuffer, SendBuffer: opts.SendBuffer})
		}); err != nil {
			return err
		}
		return applyErr
	}
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

// UnsupportedOptionError reports that a requested option is unavailable:
// Platform is the operating system, or "network <name>" when the option
// does not apply to that kind of socket.
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
		if err := checkNetwork(network, opts); err != nil {
			return err
		}
		if opts == (Options{}) {
			return nil
		}
		var applyErr error
		if err := raw.Control(func(fd uintptr) {
			if applyErr = applyBuffers(fd, opts); applyErr == nil {
				applyErr = applyOptions(fd, opts, outbound)
			}
		}); err != nil {
			return err
		}
		return applyErr
	}
}

// checkNetwork refuses options that cannot apply to the network instead of
// dropping them silently: any option on a network other than TCP or UDP,
// and the TCP-only options (Fast Open, keep-alive) on UDP. This is a
// configuration error and always fails explicitly.
func checkNetwork(network string, o Options) error {
	isTCP := network == "tcp" || network == "tcp4" || network == "tcp6"
	isUDP := network == "udp" || network == "udp4" || network == "udp6"
	where := "network " + network
	switch {
	case !isTCP && !isUDP:
		if o != (Options{}) {
			return &UnsupportedOptionError{Option: "socket options", Platform: where}
		}
	case isUDP && o.FastOpen.Enabled:
		return &UnsupportedOptionError{Option: "TCP Fast Open", Platform: where}
	case isUDP && !o.KeepAlive.IsZero():
		return &UnsupportedOptionError{Option: "TCP keep-alive", Platform: where}
	}
	return nil
}
