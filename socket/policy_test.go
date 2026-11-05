package socket_test

import (
	"context"
	"io"
	"net"
	"runtime"
	"testing"

	"github.com/ninepeach/go-netx/socket"
)

// One configuration with Fast Open works on every platform: where Fast
// Open is unavailable the sockets run without it.
// Checks: SOCK-06
func TestFastOpenConfigurationRunsEverywhere(t *testing.T) {
	opts := socket.Options{FastOpen: socket.FastOpenOptions{Enabled: true, Backlog: 16}}
	lc := socket.ListenConfig(opts)
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen with Fast Open on %s: %v", runtime.GOOS, err)
	}
	defer ln.Close()
	go func() {
		if c, err := ln.Accept(); err == nil {
			c.Write([]byte("ok"))
			c.Close()
		}
	}()
	d := socket.Dialer(net.Dialer{}, opts)
	c, err := d.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("Dial with Fast Open on %s: %v", runtime.GOOS, err)
	}
	defer c.Close()
	b := make([]byte, 2)
	if _, err := io.ReadFull(c, b); err != nil || string(b) != "ok" {
		t.Fatalf("read %q, %v", b, err)
	}
}

// Checks: SOCK-27
func TestSupportedMatchesPlatform(t *testing.T) {
	want := map[string]socket.Capabilities{
		"linux":   {FastOpenListen: true, FastOpenDial: true, ReusePortBalanced: true},
		"darwin":  {FastOpenListen: true},
		"freebsd": {ReusePortBalanced: true},
	}[runtime.GOOS]
	if got := socket.Supported(); got != want {
		t.Fatalf("Supported() on %s = %+v, want %+v", runtime.GOOS, got, want)
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
