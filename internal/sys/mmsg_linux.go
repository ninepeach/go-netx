package sys

import (
	"net"
	"net/netip"
	"os"
	"strconv"
	"syscall"
	"unsafe"
)

// BatchSupported reports whether BatchReader receives more than one
// datagram per system call (Linux recvmmsg).
const BatchSupported = true

// DefaultBatch is the batch size udp uses when Options.Batch is zero.
const DefaultBatch = 8

// mmsghdr mirrors struct mmsghdr; Go pads it to the platform's alignment
// as C does.
type mmsghdr struct {
	hdr syscall.Msghdr
	len uint32
}

// BatchReader receives up to len(bufs) datagrams per recvmmsg call into
// caller-owned buffers, without allocating per call.
type BatchReader struct {
	raw     syscall.RawConn
	msgs    []mmsghdr
	iovs    []syscall.Iovec
	names   []syscall.RawSockaddrAny
	oob     []byte
	oobSize int
	n       int
	errno   syscall.Errno
	readFn  func(fd uintptr) bool // built once so Read does not allocate
	zones   map[uint32]string     // IPv6 scope ID → zone name, per reader
}

// NewBatchReader prepares one recvmmsg slot per buffer, each with oobSize
// bytes of control data. The buffers stay owned by the caller.
func NewBatchReader(raw syscall.RawConn, bufs [][]byte, oobSize int) *BatchReader {
	n := len(bufs)
	r := &BatchReader{
		raw:     raw,
		msgs:    make([]mmsghdr, n),
		iovs:    make([]syscall.Iovec, n),
		names:   make([]syscall.RawSockaddrAny, n),
		oob:     make([]byte, n*oobSize),
		oobSize: oobSize,
	}
	for i := range r.msgs {
		r.iovs[i].Base = &bufs[i][0]
		r.iovs[i].SetLen(len(bufs[i]))
		h := &r.msgs[i].hdr
		h.Iov = &r.iovs[i]
		h.Iovlen = 1
		h.Name = (*byte)(unsafe.Pointer(&r.names[i]))
		if oobSize > 0 {
			h.Control = &r.oob[i*oobSize]
		}
	}
	r.readFn = r.recv
	return r
}

func (r *BatchReader) recv(fd uintptr) bool {
	for i := range r.msgs {
		h := &r.msgs[i].hdr
		h.Namelen = syscall.SizeofSockaddrAny
		h.SetControllen(r.oobSize)
		h.Flags = 0
	}
	for {
		n, _, e := syscall.Syscall6(syscall.SYS_RECVMMSG, fd,
			uintptr(unsafe.Pointer(&r.msgs[0])), uintptr(len(r.msgs)), 0, 0, 0)
		switch e {
		case syscall.EINTR:
			continue
		case syscall.EAGAIN:
			return false // wait for the poller
		}
		r.n, r.errno = int(n), e
		return true
	}
}

// Read blocks until at least one datagram arrives and returns how many
// were received. Errors are *os.SyscallError wrapping the errno, or the
// net package's error after Close.
func (r *BatchReader) Read() (int, error) {
	r.n, r.errno = 0, 0
	if err := r.raw.Read(r.readFn); err != nil {
		return 0, err
	}
	if r.errno != 0 {
		return 0, os.NewSyscallError("recvmmsg", r.errno)
	}
	return r.n, nil
}

// Message returns datagram i of the last Read: its length, control data,
// flags (MsgTrunc) and source address with IPv4-mapped addresses unmapped.
func (r *BatchReader) Message(i int) (n int, oob []byte, flags int, from netip.AddrPort) {
	m := &r.msgs[i]
	if r.oobSize > 0 {
		oob = r.oob[i*r.oobSize : i*r.oobSize+int(m.hdr.Controllen)]
	}
	return int(m.len), oob, int(m.hdr.Flags), r.addr(&r.names[i])
}

func (r *BatchReader) addr(sa *syscall.RawSockaddrAny) netip.AddrPort {
	switch sa.Addr.Family {
	case syscall.AF_INET:
		in := (*syscall.RawSockaddrInet4)(unsafe.Pointer(sa))
		return netip.AddrPortFrom(netip.AddrFrom4(in.Addr), port(in.Port))
	case syscall.AF_INET6:
		in := (*syscall.RawSockaddrInet6)(unsafe.Pointer(sa))
		a := netip.AddrFrom16(in.Addr).Unmap()
		if in.Scope_id != 0 && a.Is6() {
			a = a.WithZone(r.zone(in.Scope_id))
		}
		return netip.AddrPortFrom(a, port(in.Port))
	}
	return netip.AddrPort{}
}

// zone names a scope ID as the net package does: the interface name, or
// the number when the interface is unknown. Only link-local senders have
// one, and the result is cached so the lookup runs once per interface.
func (r *BatchReader) zone(id uint32) string {
	if z, ok := r.zones[id]; ok {
		return z
	}
	z := strconv.FormatUint(uint64(id), 10)
	if ifi, err := net.InterfaceByIndex(int(id)); err == nil {
		z = ifi.Name
	}
	if r.zones == nil {
		r.zones = map[uint32]string{}
	}
	r.zones[id] = z
	return z
}

// port converts a sockaddr port from network byte order.
func port(p uint16) uint16 {
	b := (*[2]byte)(unsafe.Pointer(&p))
	return uint16(b[0])<<8 | uint16(b[1])
}
