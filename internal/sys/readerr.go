package sys

// ReadErr classifies a datagram read error.
type ReadErr uint8

const (
	// Fatal: the socket is unusable; stop reading and report the error.
	Fatal ReadErr = iota
	// Transient: retry after a short backoff.
	Transient
	// Truncated: the datagram exceeded the buffer and was discarded.
	Truncated
)
