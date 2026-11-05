//go:build linux

package socket

import "syscall"

var platformCapabilities = Capabilities{FastOpenListen: true, FastOpenDial: true, ReusePortBalanced: true}

const (
	tcpFastOpen        = 23
	tcpFastOpenConnect = 30
	soReusePort        = 15
)

func applyOptions(fd uintptr, opts Options, outbound bool) error {
	if opts.ReusePort {
		if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, soReusePort, 1); err != nil {
			return err
		}
	}
	if !opts.FastOpen.Enabled {
		return nil
	}
	value, option := opts.FastOpen.Backlog, tcpFastOpen
	if value <= 0 {
		value = 128
	}
	if outbound {
		value, option = 1, tcpFastOpenConnect
	}
	return fastOpenResult(syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, option, value))
}
