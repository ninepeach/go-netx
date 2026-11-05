package sys

// Winsock error codes that UDP reads report.
const (
	wsaeMsgSize   = 10040
	wsaeNetReset  = 10052
	wsaeConnReset = 10054
)

// classifyWinsock is the Windows read-error policy as a pure function, so
// it is compiled and tested on every platform.
func classifyWinsock(code uint32) ReadErr {
	switch code {
	case wsaeMsgSize:
		return Truncated
	case wsaeConnReset, wsaeNetReset:
		return Transient
	}
	return Fatal
}
