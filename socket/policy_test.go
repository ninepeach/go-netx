package socket_test

import (
	"context"
	"net"
	"runtime"
	"testing"

	"github.com/ninepeach/go-netx/socket"
)

// Runs on every platform, including Windows, where options_test.go
// (unix-only) does not build.
// Checks: SOCK-06, SOCK-08
func TestFastOpenPolicy(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("requires kernel-specific expectations")
	}
	lc := socket.ListenConfig(socket.Options{FastOpen: socket.FastOpenOptions{Enabled: true, Unsupported: socket.UnsupportedError}})
	_, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err == nil || !socket.IsUnsupported(err) {
		t.Fatalf("expected unsupported error, got %v", err)
	}

	lc = socket.ListenConfig(socket.Options{FastOpen: socket.FastOpenOptions{Enabled: true, Unsupported: socket.UnsupportedIgnore}})
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
}

// TCP-only options on UDP are a configuration error on every platform and
// are not covered by UnsupportedIgnore.
//
// Checks: SOCK-18
func TestTCPOnlyOptionsOnUDPAreRefused(t *testing.T) {
	for _, o := range []socket.Options{
		{FastOpen: socket.FastOpenOptions{Enabled: true}},
		{FastOpen: socket.FastOpenOptions{Enabled: true, Unsupported: socket.UnsupportedIgnore}},
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
