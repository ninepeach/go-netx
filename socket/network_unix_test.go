//go:build unix

package socket_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ninepeach/go-netx/socket"
)

// Checks: SOCK-17
func TestOptionsOnOtherNetworksAreRefused(t *testing.T) {
	for _, o := range []socket.Options{
		{RecvBuffer: 4096},
		{ReusePort: true},
		{KeepAlive: socket.KeepAlive{Disable: true}},
		{FastOpen: socket.FastOpenOptions{Enabled: true}},
	} {
		lc := socket.ListenConfig(o)
		ln, err := lc.Listen(context.Background(), "unix", filepath.Join(t.TempDir(), "s.sock"))
		if err == nil {
			ln.Close()
			t.Fatalf("%+v on a unix socket was silently ignored", o)
		}
		if !socket.IsUnsupported(err) {
			t.Fatalf("%+v: %v, want UnsupportedOptionError", o, err)
		}
	}
}

// Checks: SOCK-19
func TestZeroOptionsWorkOnAnyNetwork(t *testing.T) {
	lc := socket.ListenConfig(socket.Options{})
	ln, err := lc.Listen(context.Background(), "unix", filepath.Join(t.TempDir(), "s.sock"))
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
	pc, err := lc.ListenPacket(context.Background(), "udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	pc.Close()
}
