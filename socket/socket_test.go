package socket

import (
	"context"
	"errors"
	"strings"
	"syscall"
	"testing"
)

type rawStub struct {
	called bool
	err    error
}

func (c *rawStub) Control(f func(uintptr)) error {
	c.called = true
	if c.err == nil {
		f(^uintptr(0))
	}
	return c.err
}
func (*rawStub) Read(func(uintptr) bool) error  { panic("unexpected Read") }
func (*rawStub) Write(func(uintptr) bool) error { panic("unexpected Write") }

func TestControlErrors(t *testing.T) {
	c := &rawStub{}
	if err := ListenControl(Options{})("tcp4", "", c); err != nil || c.called {
		t.Fatalf("zero options: called=%t, err=%v", c.called, err)
	}
	if err := DialControl(Options{FastOpen: true})(context.Background(), "udp6", "", c); !errors.Is(err, errors.ErrUnsupported) || c.called {
		t.Fatalf("UDP FastOpen: called=%t, err=%v", c.called, err)
	}
	c.err = errors.New("raw control error")
	hook := ListenControl(Options{ReusePort: true})
	if err := hook("tcp4", "", c); !errors.Is(err, c.err) {
		t.Fatalf("raw error: %v", err)
	}
	c.err = nil
	// The stub has no real descriptor: the option fails, and the failure
	// surfaces from the hook naming the option.
	if err := hook("tcp4", "", c); err == nil || !strings.Contains(err.Error(), "ReusePort") {
		t.Fatalf("option error: %v", err)
	}
}

var _ syscall.RawConn = (*rawStub)(nil)
