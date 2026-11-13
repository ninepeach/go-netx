//go:build !linux && !darwin

package udp

import "net"

func waitPacket(*net.UDPConn) error { return nil }
