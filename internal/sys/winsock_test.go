package sys

import "testing"

// Checks: SYS-02
func TestClassifyWinsock(t *testing.T) {
	for code, want := range map[uint32]ReadErr{
		wsaeMsgSize:   Truncated, // oversized datagram
		wsaeConnReset: Transient, // ICMP port unreachable on an unconnected socket
		wsaeNetReset:  Transient,
		10038:         Fatal, // WSAENOTSOCK
		10022:         Fatal, // WSAEINVAL
		0:             Fatal,
	} {
		if got := classifyWinsock(code); got != want {
			t.Errorf("code %d: got %d want %d", code, got, want)
		}
	}
}
