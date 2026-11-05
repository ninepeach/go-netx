package leak

import (
	"net"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Checks: NF-18
func TestDetectsLeakedGoroutineAndFD(t *testing.T) {
	base := Take()
	stop := make(chan struct{})
	go func() { <-stop }()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	err = base.Check(50*time.Millisecond, 0)
	if err == nil || !strings.Contains(err.Error(), "goroutines") {
		t.Fatalf("leak not reported: %v", err)
	}
	if runtime.GOOS == "linux" && Take().FDs <= base.FDs {
		t.Fatal("FD not counted")
	}
	close(stop)
	ln.Close()
	if err := base.Check(2*time.Second, 0); err != nil {
		t.Fatalf("clean state reported as leak: %v", err)
	}
}
