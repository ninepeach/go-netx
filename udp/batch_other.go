//go:build !darwin && (!linux || (!amd64 && !arm64))

package udp

import "net"

func readBatch(*net.UDPConn, []Packet, int) (int, error, bool) { return 0, nil, false }
func writeBatch(*net.UDPConn, []Packet) (int, error, bool)     { return 0, nil, false }
