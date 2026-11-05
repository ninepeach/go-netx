package socket_test

import (
	"context"
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
