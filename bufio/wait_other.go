//go:build !linux && !darwin

package bufio

import "net"

func tcpReadWaiter(*net.TCPConn) (ReadWaiter, bool) { return nil, false }
