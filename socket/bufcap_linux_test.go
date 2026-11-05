package socket_test

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/ninepeach/go-netx/socket"
)

func sysctlInt(t *testing.T, name string) int {
	t.Helper()
	b, err := os.ReadFile("/proc/sys/net/core/" + name)
	if err != nil {
		t.Fatal(err)
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// canForceBuffers reports whether this process may exceed rmem_max
// (CAP_NET_ADMIN), by trying it on a probe socket.
func canForceBuffers(t *testing.T, over int) bool {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	raw, _ := c.SyscallConn()
	var got int
	raw.Control(func(fd uintptr) {
		_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUFFORCE, over)
		got, _ = syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF)
	})
	return got/2 >= over
}

func readBuf(t *testing.T, c *net.UDPConn, opt int) int {
	t.Helper()
	raw, _ := c.SyscallConn()
	var v int
	var err error
	raw.Control(func(fd uintptr) { v, err = syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, opt) })
	if err != nil {
		t.Fatal(err)
	}
	return v / 2
}

// expectBuffer checks the outcome of requesting over the cap: the full size
// with CAP_NET_ADMIN, otherwise a BufferSizeError naming the cap.
func expectBuffer(t *testing.T, err error, force bool, option string, requested, limit int) {
	t.Helper()
	if force {
		if err != nil {
			t.Fatalf("%s %d with CAP_NET_ADMIN: %v", option, requested, err)
		}
		return
	}
	var bse *socket.BufferSizeError
	if !errors.As(err, &bse) {
		t.Fatalf("%s %d over the %d cap: err = %v, want *BufferSizeError", option, requested, limit, err)
	}
	if bse.Option != option || bse.Requested != requested || bse.Actual != limit {
		t.Fatalf("got %+v, want %s %d/%d", *bse, option, requested, limit)
	}
}

// Checks: SOCK-24, SOCK-25
func TestBufferCapOnLinux(t *testing.T) {
	rmax, wmax := sysctlInt(t, "rmem_max"), sysctlInt(t, "wmem_max")
	overR, overW := rmax+1<<20, wmax+1<<20
	force := canForceBuffers(t, overR)
	t.Logf("rmem_max=%d wmem_max=%d CAP_NET_ADMIN=%v", rmax, wmax, force)

	// Within the cap: granted and verified.
	lc := socket.ListenConfig(socket.Options{RecvBuffer: rmax, SendBuffer: wmax})
	pc, err := lc.ListenPacket(context.Background(), "udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("buffers at the cap: %v", err)
	}
	uc := pc.(*net.UDPConn)
	if r, w := readBuf(t, uc, syscall.SO_RCVBUF), readBuf(t, uc, syscall.SO_SNDBUF); r < rmax || w < wmax {
		t.Fatalf("granted %d/%d, want at least %d/%d", r, w, rmax, wmax)
	}
	pc.Close()

	// Over the cap, through ListenConfig (SOCK-24).
	for _, tc := range []struct {
		opts     socket.Options
		option   string
		req, lim int
		sockOpt  int
	}{
		{socket.Options{RecvBuffer: overR}, "receive buffer", overR, rmax, syscall.SO_RCVBUF},
		{socket.Options{SendBuffer: overW}, "send buffer", overW, wmax, syscall.SO_SNDBUF},
	} {
		lc := socket.ListenConfig(tc.opts)
		pc, err := lc.ListenPacket(context.Background(), "udp4", "127.0.0.1:0")
		expectBuffer(t, err, force, tc.option, tc.req, tc.lim)
		if err == nil {
			if got := readBuf(t, pc.(*net.UDPConn), tc.sockOpt); got < tc.req {
				t.Fatalf("%s: granted %d, want %d", tc.option, got, tc.req)
			}
			pc.Close()
		}
	}

	// Over the cap on an existing connection, through SetBuffers (SOCK-25).
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	expectBuffer(t, socket.SetBuffers(c, socket.Options{RecvBuffer: overR}), force, "receive buffer", overR, rmax)
	expectBuffer(t, socket.SetBuffers(c, socket.Options{SendBuffer: overW}), force, "send buffer", overW, wmax)
}
