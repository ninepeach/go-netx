//go:build !linux

package udp

import "net"

// OffloadSupport reports whether the kernel exposes UDP offload options.
func OffloadSupport(*net.UDPConn) (bool, bool, error) { return false, false, nil }
