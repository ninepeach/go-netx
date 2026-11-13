package socket

import (
	"context"
	"errors"
	"net"
	"syscall"
	"testing"
)

func TestLinuxInterfaceAndMark(t *testing.T) {
	for _, tc := range []struct {
		name      string
		options   Options
		opt, want int
	}{
		{"interface", Options{Interface: "lo"}, syscall.SO_BINDTODEVICE, 0},
		{"mark/zero", Options{Mark: markPointer(0)}, syscall.SO_MARK, 0},
		{"mark/nonzero", Options{Mark: markPointer(123)}, syscall.SO_MARK, 123},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lc := net.ListenConfig{Control: ListenControl(tc.options)}
			c, err := lc.ListenPacket(context.Background(), "udp4", "127.0.0.1:0")
			if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
				if errors.Is(err, errors.ErrUnsupported) {
					t.Fatalf("a permission error reported as unsupported: %v", err)
				}
				t.Skipf("insufficient socket privileges: %v", err)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if tc.opt == syscall.SO_BINDTODEVICE {
				return
			}
			raw, err := c.(*net.UDPConn).SyscallConn()
			if err != nil {
				t.Fatal(err)
			}
			if err := raw.Control(func(fd uintptr) {
				value, err := syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, tc.opt)
				if err != nil || value != tc.want {
					t.Errorf("mark = %d, %v", value, err)
				}
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func markPointer(mark uint32) *uint32 { return &mark }

func TestLinuxFastOpen(t *testing.T) {
	for _, listen := range []bool{false, true} {
		fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer syscall.Close(fd)
			opt, want := 30, 1 // TCP_FASTOPEN_CONNECT
			if listen {
				opt, want = tcpFastOpen, 128
			}
			if err := apply(uintptr(fd), "tcp4", Options{FastOpen: true}, listen); err != nil {
				t.Fatal(err)
			}
			got, err := syscall.GetsockoptInt(fd, syscall.IPPROTO_TCP, opt)
			if errors.Is(err, syscall.ENOPROTOOPT) || errors.Is(err, syscall.EOPNOTSUPP) {
				return
			}
			if err != nil || got != want {
				t.Fatalf("listen=%t: FastOpen = %d, %v", listen, got, err)
			}
		}()
	}
}
