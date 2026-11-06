package netx_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ninepeach/go-netx"
)

// Checks: RUN-17
func TestOnErrorReceivesFinalErrorOnEveryPath(t *testing.T) {
	errStart, errShutdown, errStop, errServe := errors.New("start"), errors.New("shutdown"), errors.New("stop"), errors.New("serve")
	cases := []struct {
		name string
		srv  func() *netx.Server
		want []error // all must be in the error OnError gets; nil: OnError not called
	}{
		{"start fails", func() *netx.Server {
			s := netx.NewServer(netx.ServiceFuncs{ServeFunc: func(context.Context) error { return nil }})
			s.OnStart = func(context.Context) error { return errStart }
			return s
		}, []error{errStart}},
		{"start, shutdown and stop fail", func() *netx.Server {
			s := netx.NewServer(netx.ServiceFuncs{
				ServeFunc:    func(context.Context) error { return nil },
				ShutdownFunc: func(context.Context) error { return errShutdown },
			})
			s.OnStart = func(context.Context) error { return errStart }
			s.OnStop = func(context.Context) error { return errStop }
			return s
		}, []error{errStart, errShutdown, errStop}},
		{"serve fails", func() *netx.Server {
			return netx.NewServer(netx.ServiceFuncs{ServeFunc: func(context.Context) error { return errServe }})
		}, []error{errServe}},
		{"clean exit", func() *netx.Server {
			return netx.NewServer(netx.ServiceFuncs{ServeFunc: func(context.Context) error { return nil }})
		}, nil},
	}
	for _, tc := range cases {
		s := tc.srv()
		var calls int
		var got error
		s.OnError = func(err error) { calls++; got = err }
		result := s.LoopContext(context.Background())
		if tc.want == nil {
			if calls != 0 || result != nil {
				t.Fatalf("%s: OnError called %d times, result %v; want no call", tc.name, calls, result)
			}
			continue
		}
		if calls != 1 {
			t.Fatalf("%s: OnError called %d times, want 1", tc.name, calls)
		}
		if got != result {
			t.Fatalf("%s: OnError got %v, LoopContext returned %v", tc.name, got, result)
		}
		for _, w := range tc.want {
			if !errors.Is(got, w) {
				t.Fatalf("%s: OnError error %v does not include %v", tc.name, got, w)
			}
		}
	}
}

// Checks: RUN-18
func TestOnErrorPanicIsRecovered(t *testing.T) {
	errServe := errors.New("serve failed")
	for name, start := range map[string]func(context.Context) error{
		"after serve": nil,
		"start fails": func(context.Context) error { return errServe },
	} {
		s := netx.NewServer(netx.ServiceFuncs{ServeFunc: func(context.Context) error { return errServe }})
		s.OnStart = start
		s.OnError = func(error) { panic("reporter down") }
		var result error
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s: LoopContext panicked: %v", name, r)
				}
			}()
			result = s.LoopContext(context.Background())
		}()
		if !errors.Is(result, errServe) {
			t.Fatalf("%s: original error lost: %v", name, result)
		}
		if result == nil || !strings.Contains(result.Error(), "reporter down") {
			t.Fatalf("%s: panic not reported in %v", name, result)
		}
		if s.State() != netx.StateStopped {
			t.Fatalf("%s: state %v", name, s.State())
		}
	}
}
