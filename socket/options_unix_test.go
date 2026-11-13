//go:build linux || darwin

package socket

import (
	"context"
	"errors"
	"net"
	"syscall"
	"testing"
)

func TestReusePort(t *testing.T) {
	for _, tc := range []struct{ network, address, family string }{
		{"tcp4", "127.0.0.1:0", "tcp4"},
		{"tcp6", "[::1]:0", "tcp6"},
		{"udp4", "127.0.0.1:0", "udp4"},
		{"udp6", "[::1]:0", "udp6"},
		{"tcp", "[::]:0", "tcp6"},
		{"udp", "[::]:0", "udp6"},
	} {
		t.Run(tc.network, func(t *testing.T) {
			var family string
			hook := ListenControl(Options{ReusePort: true})
			lc := net.ListenConfig{Control: func(network, address string, c syscall.RawConn) error {
				family = network
				if err := hook(network, address, c); err != nil {
					return err
				}
				var value int
				var optionErr error
				if err := c.Control(func(fd uintptr) {
					value, optionErr = syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, soReusePort)
				}); err != nil {
					return err
				}
				if optionErr != nil {
					return optionErr
				}
				if value == 0 {
					t.Errorf("SO_REUSEPORT = %d", value)
				}
				return nil
			}}
			if tc.network[:3] == "tcp" {
				l, err := lc.Listen(context.Background(), tc.network, tc.address)
				if err != nil {
					t.Fatal(err)
				}
				defer l.Close()
			} else {
				c, err := lc.ListenPacket(context.Background(), tc.network, tc.address)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
			}
			if family != tc.family {
				t.Errorf("hook family = %q, want %q", family, tc.family)
			}
		})
	}
}

func TestInvalidInterface(t *testing.T) {
	lc := net.ListenConfig{Control: ListenControl(Options{Interface: "netx-no-such"})}
	c, err := lc.ListenPacket(context.Background(), "udp4", "127.0.0.1:0")
	if c != nil {
		c.Close()
	}
	// A wrong name is a bad request, not a missing capability.
	if err == nil || errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("invalid interface: %v", err)
	}
}
