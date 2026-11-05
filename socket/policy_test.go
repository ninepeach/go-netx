package socket_test

import (
	"context"
	"net"
	"runtime"
	"testing"

	"github.com/ninepeach/go-netx/socket"
)

// Fast Open must fail explicitly on platforms where it is unavailable.
// Checks: SOCK-06, SOCK-08
func TestFastOpenUnsupportedPlatforms(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("Linux support depends on the running kernel; covered by Linux tests")
	}
	lc := socket.ListenConfig(socket.Options{FastOpen: socket.FastOpenOptions{Enabled: true}})
	_, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err == nil || !socket.IsUnsupported(err) {
		t.Fatalf("expected unsupported error, got %v", err)
	}
}

// TCP-only options on UDP are a configuration error on every platform.
// Checks: SOCK-18
func TestTCPOnlyOptionsOnUDPAreRefused(t *testing.T) {
	for _, o := range []socket.Options{
		{FastOpen: socket.FastOpenOptions{Enabled: true}},
		{KeepAlive: socket.KeepAlive{Idle: 1}},
		{KeepAlive: socket.KeepAlive{Disable: true}},
	} {
		lc := socket.ListenConfig(o)
		pc, err := lc.ListenPacket(context.Background(), "udp", "127.0.0.1:0")
		if err == nil {
			pc.Close()
			t.Fatalf("ListenPacket %+v: silently ignored", o)
		}
		if !socket.IsUnsupported(err) {
			t.Fatalf("ListenPacket %+v: %v", o, err)
		}
		d := socket.Dialer(net.Dialer{}, o)
		c, err := d.DialContext(context.Background(), "udp", "127.0.0.1:9")
		if err == nil {
			c.Close()
			t.Fatalf("Dial %+v: silently ignored", o)
		}
		if !socket.IsUnsupported(err) {
			t.Fatalf("Dial %+v: %v", o, err)
		}
	}
	// Options that do apply to UDP still work.
	lc := socket.ListenConfig(socket.Options{RecvBuffer: 1 << 16})
	pc, err := lc.ListenPacket(context.Background(), "udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	pc.Close()
}
