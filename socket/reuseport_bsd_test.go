//go:build darwin || freebsd

package socket_test

import (
	"context"
	"net"
	"runtime"
	"syscall"
	"testing"

	"github.com/ninepeach/go-netx/socket"
)

func reusePortOption() int {
	if runtime.GOOS == "freebsd" {
		return 0x00010000 // SO_REUSEPORT_LB
	}
	return syscall.SO_REUSEPORT
}

// Checks: SOCK-20, SOCK-21
func TestReusePortOnBSD(t *testing.T) {
	lc := socket.ListenConfig(socket.Options{ReusePort: true})
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if v := socketIntOption(t, ln.(*net.TCPListener), syscall.SOL_SOCKET, reusePortOption()); v == 0 {
		t.Fatalf("reuse-port option not set on %s", runtime.GOOS)
	}
	second, err := lc.Listen(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal("second bind on the same port failed:", err)
	}
	second.Close()
	pc, err := lc.ListenPacket(context.Background(), "udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	pc2, err := lc.ListenPacket(context.Background(), "udp", pc.LocalAddr().String())
	if err != nil {
		t.Fatal("second UDP bind failed:", err)
	}
	pc2.Close()
}
