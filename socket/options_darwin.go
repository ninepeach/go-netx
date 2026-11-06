//go:build darwin

package socket

import "syscall"

// tcpFastOpenDarwin is TCP_FASTOPEN from <netinet/tcp.h> (macOS 10.11+),
// which the syscall package does not define. It enables server-side Fast
// Open on a listener; its value is a boolean, so Backlog is not used.
const tcpFastOpenDarwin = 0x105

var platformCapabilities = Capabilities{FastOpenListen: true}

// macOS accepts SO_REUSEPORT but does not balance load across the sockets:
// one of them receives everything. It is supported so the same
// configuration runs on a development Mac and a Linux server.
func applyOptions(fd uintptr, opts Options) error {
	if opts.ReusePort {
		if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEPORT, 1); err != nil {
			return err
		}
	}
	if opts.FastOpen.Enabled {
		return fastOpenResult(syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, tcpFastOpenDarwin, 1))
	}
	return nil
}
