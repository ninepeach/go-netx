//go:build linux && (amd64 || arm64)

package udp

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ninepeach/go-netx/buf"
)

const batchLimit = 32

type multiMessage struct {
	msg    syscall.Msghdr
	length uint32
}

type batchWorkspace struct {
	msgs                  [batchLimit]multiMessage
	vectors               [batchLimit]syscall.Iovec
	names                 [batchLimit]syscall.RawSockaddrAny
	buffers               [batchLimit]*buf.Buf
	readReady, writeReady func(uintptr) bool
	size, count, max      int
	errno                 syscall.Errno
	probeErr              syscall.Errno
}

var batchPool = sync.Pool{New: func() any {
	w := new(batchWorkspace)
	w.readReady, w.writeReady = w.read, w.write
	return w
}}

func (w *batchWorkspace) release() {
	for _, b := range w.buffers {
		if b != nil {
			b.Release()
		}
	}
	*w = batchWorkspace{readReady: w.readReady, writeReady: w.writeReady}
	batchPool.Put(w)
}

func (w *batchWorkspace) prepare(i int, p []byte) {
	v := &w.vectors[i]
	v.Base = unsafe.SliceData(p)
	v.SetLen(len(p))
	w.msgs[i].msg.Iov = v
	w.msgs[i].msg.Iovlen = 1
}

func readBatch(c *net.UDPConn, dst []Packet, max int) (int, error, bool) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, err, true
	}
	size := min(len(dst), batchLimit)
	w := batchPool.Get().(*batchWorkspace)
	defer w.release()
	w.size, w.max = size, max
	err = raw.Read(w.readReady)
	if err != nil {
		return 0, err, true
	}
	if w.probeErr != 0 {
		return 0, os.NewSyscallError("recvfrom", w.probeErr), true
	}
	if unsupportedBatch(w.errno) {
		return 0, nil, false
	}
	if w.errno != 0 {
		return 0, os.NewSyscallError("recvmmsg", w.errno), true
	}
	n := 0
	for i := range w.count {
		m := &w.msgs[i]
		if int(m.length) > max || m.msg.Flags&syscall.MSG_TRUNC != 0 {
			err = ErrTruncated
			continue
		}
		addr, addrErr := batchPeer(c, &w.names[i])
		if addrErr != nil {
			err = errors.Join(err, addrErr)
			continue
		}
		b := w.buffers[i]
		w.buffers[i] = nil
		dst[n] = finishPacket(b, int(m.length), max, addr)
		n++
	}
	return n, err, true
}

func (w *batchWorkspace) read(fd uintptr) bool {
	if w.buffers[0] == nil {
		var probe byte
		for {
			_, _, w.probeErr = syscall.Syscall6(syscall.SYS_RECVFROM, fd, uintptr(unsafe.Pointer(&probe)), 1, syscall.MSG_PEEK|syscall.MSG_DONTWAIT, 0, 0)
			if w.probeErr != syscall.EINTR {
				break
			}
		}
		if w.probeErr != 0 {
			return w.probeErr != syscall.EAGAIN
		}
		for i := range w.size {
			b := buf.Get(w.max + 1)
			w.buffers[i] = b
			w.prepare(i, b.FreeBytes())
			w.msgs[i].msg.Name = (*byte)(unsafe.Pointer(&w.names[i]))
			w.msgs[i].msg.Namelen = uint32(unsafe.Sizeof(w.names[i]))
		}
	}
	for {
		w.count, w.errno = mmsg(syscall.SYS_RECVMMSG, fd, w.msgs[:w.size])
		if w.errno != syscall.EINTR {
			return w.errno != syscall.EAGAIN
		}
	}
}

func writeBatch(c *net.UDPConn, packets []Packet) (int, error, bool) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, err, true
	}
	connected := c.RemoteAddr() != nil
	ipv6 := false
	if !connected {
		var socketErr error
		err = raw.Control(func(fd uintptr) {
			addr, e := syscall.Getsockname(int(fd))
			_, ipv6 = addr.(*syscall.SockaddrInet6)
			socketErr = e
		})
		if err != nil {
			return 0, err, true
		}
		if socketErr != nil {
			return 0, os.NewSyscallError("getsockname", socketErr), true
		}
	}
	w := batchPool.Get().(*batchWorkspace)
	defer w.release()
	total := 0
	for total < len(packets) {
		batch := packets[total : total+min(batchLimit, len(packets)-total)]
		for i, p := range batch {
			w.prepare(i, p.Buffer.Bytes())
			if !connected {
				nameLen, err := encodeName(&w.names[i], p.Addr, ipv6)
				if err != nil {
					// Process the prefix first, then let the standard path report this address.
					batch = batch[:i]
					break
				}
				w.msgs[i].msg.Name = (*byte)(unsafe.Pointer(&w.names[i]))
				w.msgs[i].msg.Namelen = nameLen
			}
		}
		if len(batch) == 0 {
			if err := WritePacket(c, packets[total]); err != nil {
				return total, err, true
			}
			total++
			continue
		}
		w.count, w.size, w.errno = 0, len(batch), 0
		err = raw.Write(w.writeReady)
		total += w.count
		runtime.KeepAlive(batch)
		if err != nil {
			return total, err, true
		}
		if unsupportedBatch(w.errno) && total == 0 {
			return 0, nil, false
		}
		if w.errno != 0 {
			return total, os.NewSyscallError("sendmmsg", w.errno), true
		}
		for i := range batch {
			if int(w.msgs[i].length) != batch[i].Buffer.Len() {
				return total, io.ErrShortWrite, true
			}
		}
	}
	return total, nil, true
}

func (w *batchWorkspace) write(fd uintptr) bool {
	for w.count < w.size {
		n, errno := mmsg(sendmmsgTrap, fd, w.msgs[w.count:w.size])
		w.errno = errno
		w.count += n
		if errno == syscall.EAGAIN {
			return false
		}
		if errno != 0 && errno != syscall.EINTR {
			return true
		}
		if errno == 0 && n == 0 {
			w.errno = syscall.EIO
			return true
		}
	}
	return true
}

func unsupportedBatch(errno syscall.Errno) bool {
	return errno == syscall.ENOSYS || errno == syscall.EOPNOTSUPP
}

func mmsg(trap, fd uintptr, msgs []multiMessage) (int, syscall.Errno) {
	n, _, errno := syscall.Syscall6(trap, fd, uintptr(unsafe.Pointer(&msgs[0])), uintptr(len(msgs)), syscall.MSG_DONTWAIT, 0, 0)
	if errno != 0 {
		return 0, errno
	}
	return int(n), 0
}

func encodeName(name *syscall.RawSockaddrAny, peer netip.AddrPort, ipv6 bool) (uint32, error) {
	ip := peer.Addr().Unmap()
	if !ip.IsValid() {
		return 0, syscall.EINVAL
	}
	if ipv6 {
		addr := (*syscall.RawSockaddrInet6)(unsafe.Pointer(name))
		*addr = syscall.RawSockaddrInet6{Family: syscall.AF_INET6, Addr: ip.As16()}
		encodePort(&addr.Port, peer.Port())
		if zone := ip.Zone(); zone != "" {
			if i, err := strconv.ParseUint(zone, 10, 32); err == nil {
				addr.Scope_id = uint32(i)
			} else {
				iface, err := net.InterfaceByName(zone)
				if err != nil {
					return 0, err
				}
				addr.Scope_id = uint32(iface.Index)
			}
		}
		return uint32(unsafe.Sizeof(*addr)), nil
	}
	if !ip.Is4() {
		return 0, syscall.EAFNOSUPPORT
	}
	addr := (*syscall.RawSockaddrInet4)(unsafe.Pointer(name))
	*addr = syscall.RawSockaddrInet4{Family: syscall.AF_INET, Addr: ip.As4()}
	encodePort(&addr.Port, peer.Port())
	return uint32(unsafe.Sizeof(*addr)), nil
}

func encodePort(p *uint16, n uint16) {
	binary.BigEndian.PutUint16(unsafe.Slice((*byte)(unsafe.Pointer(p)), 2), n)
}
