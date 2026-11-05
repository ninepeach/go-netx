//go:build !linux

package sys

import (
	"errors"
	"net/netip"
	"syscall"
)

// BatchSupported is false: without recvmmsg each read returns one datagram.
const BatchSupported = false

// DefaultBatch is 1 where batching is unavailable.
const DefaultBatch = 1

// MaxBatch is 1 where batching is unavailable.
const MaxBatch = 1

// BatchReader is unavailable on this platform; udp never creates one.
type BatchReader struct{}

func NewBatchReader(syscall.RawConn, [][]byte, int) *BatchReader { return &BatchReader{} }

func (*BatchReader) Read() (int, error) {
	return 0, errors.New("sys: recvmmsg is not available on this platform")
}

func (*BatchReader) Message(int) (int, []byte, int, netip.AddrPort) {
	return 0, nil, 0, netip.AddrPort{}
}
