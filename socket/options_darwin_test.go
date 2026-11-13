package socket

import (
	"context"
	"errors"
	"net"
	"syscall"
	"testing"
)

func TestDarwinInterface(t *testing.T) {
	iface, err := net.InterfaceByName("lo0")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		network, address string
		level, opt       int
	}{
		{"udp4", "127.0.0.1:0", syscall.IPPROTO_IP, syscall.IP_BOUND_IF},
		{"udp6", "[::1]:0", syscall.IPPROTO_IPV6, syscall.IPV6_BOUND_IF},
	} {
		lc := net.ListenConfig{Control: ListenControl(Options{Interface: iface.Name})}
		c, err := lc.ListenPacket(context.Background(), tc.network, tc.address)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		raw, err := c.(*net.UDPConn).SyscallConn()
		if err != nil {
			t.Fatal(err)
		}
		var value int
		var readErr error
		if err := raw.Control(func(fd uintptr) {
			value, readErr = syscall.GetsockoptInt(int(fd), tc.level, tc.opt)
		}); err != nil {
			t.Fatal(err)
		}
		if readErr != nil || value != iface.Index {
			t.Fatalf("%s interface = %d, %v", tc.network, value, readErr)
		}
	}
}

func TestDarwinMarkUnsupported(t *testing.T) {
	mark := uint32(0)
	lc := net.ListenConfig{Control: ListenControl(Options{Mark: &mark})}
	c, err := lc.ListenPacket(context.Background(), "udp4", "127.0.0.1:0")
	if c != nil {
		c.Close()
	}
	if !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Mark: %v", err)
	}
}
