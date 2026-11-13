//go:build !linux && !darwin

package socket

import (
	"errors"
	"testing"
)

func TestUnsupportedOptions(t *testing.T) {
	mark := uint32(0)
	for _, o := range []Options{{Interface: "loopback"}, {Mark: &mark}, {ReusePort: true}} {
		if err := ListenControl(o)("tcp4", "", &rawStub{}); !errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("%+v: %v", o, err)
		}
	}
	if err := ListenControl(Options{FastOpen: true})("tcp4", "", &rawStub{}); err != nil {
		t.Fatal(err)
	}
}
