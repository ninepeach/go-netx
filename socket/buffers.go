package socket

import "fmt"

// BufferSizeError reports that the kernel granted a smaller socket buffer
// than RecvBuffer or SendBuffer requested. Kernels cap these sizes without
// failing setsockopt (Linux at net.core.rmem_max and wmem_max), so go-netx
// reads the size back and returns this error instead of running with a
// buffer the caller did not ask for.
type BufferSizeError struct {
	Option    string // "receive buffer" or "send buffer"
	Requested int    // bytes
	Actual    int    // bytes the kernel granted, in the units Requested uses
}

func (e *BufferSizeError) Error() string {
	msg := fmt.Sprintf("socket: %s: requested %d bytes, kernel granted %d", e.Option, e.Requested, e.Actual)
	if hint := bufferHint(e.Option); hint != "" {
		msg += " (" + hint + ")"
	}
	return msg
}

// bufferOps abstracts setsockopt and getsockopt for one buffer so the
// verification logic is the same, and tested the same, on every platform.
type bufferOps struct {
	set func(opt, value int) error
	get func(opt int) (int, error)
}

// bufferKind describes one of the two buffers on the current platform.
type bufferKind struct {
	name  string
	opt   int // SO_RCVBUF or SO_SNDBUF
	force int // Linux SO_RCVBUFFORCE or SO_SNDBUFFORCE; 0 when unavailable
}

// ensureBuffer sets the buffer to requested bytes and checks what the kernel
// granted. Linux reports twice the usable size (the doubling covers
// bookkeeping overhead), so doubled halves the reading. When the size is
// short and a force option exists (it needs CAP_NET_ADMIN), it is tried
// once; its own error is ignored because the size read afterwards decides.
func ensureBuffer(ops bufferOps, k bufferKind, requested int, doubled bool) error {
	if err := ops.set(k.opt, requested); err != nil {
		return err
	}
	actual, err := readBuffer(ops, k, doubled)
	if err != nil {
		return err
	}
	if actual < requested && k.force != 0 {
		_ = ops.set(k.force, requested)
		if actual, err = readBuffer(ops, k, doubled); err != nil {
			return err
		}
	}
	if actual < requested {
		return &BufferSizeError{Option: k.name, Requested: requested, Actual: actual}
	}
	return nil
}

func readBuffer(ops bufferOps, k bufferKind, doubled bool) (int, error) {
	v, err := ops.get(k.opt)
	if doubled {
		v /= 2
	}
	return v, err
}
