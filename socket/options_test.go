package socket_test

import (
	"context"
	"runtime"
	"testing"

	"github.com/ninepeach/go-netx/socket"
)

func TestListenConfigWithoutOptions(t *testing.T) {
	lc := socket.ListenConfig(socket.Options{})
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
}

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
