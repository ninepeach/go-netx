package udp

import (
	"errors"
	"net"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ninepeach/go-netx/buf"
)

// XNU's extended message header uses a separate payload length.
type darwinMessage struct {
	name       *byte
	nameLen    uint32
	iov        *syscall.Iovec
	iovLen     int32
	control    *byte
	controlLen uint32
	flags      int32
	dataLen    uint64
}

const darwinBatchLimit = 32
const (
	recvmsgX = 480
	sendmsgX = 481
)

type darwinWorkspace struct {
	msgs    [darwinBatchLimit]darwinMessage
	iovs    [darwinBatchLimit]syscall.Iovec
	names   [darwinBatchLimit]syscall.RawSockaddrAny
	buffers [darwinBatchLimit]*buf.Buf
}

var darwinPool = sync.Pool{New: func() any { return new(darwinWorkspace) }}

func readBatch(c *net.UDPConn, dst []Packet, max int) (int, error, bool) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, err, true
	}
	size := min(len(dst), darwinBatchLimit)
	w := darwinPool.Get().(*darwinWorkspace)
	defer w.release()
	var count int
	var errno syscall.Errno
	var probeErr error
	err = raw.Read(func(fd uintptr) bool {
		if w.buffers[0] == nil {
			var probe [1]byte
			for {
				_, _, probeErr = syscall.Recvfrom(int(fd), probe[:], syscall.MSG_PEEK)
				if probeErr != syscall.EINTR {
					break
				}
			}
			if probeErr == syscall.EAGAIN {
				return false
			}
			if probeErr != nil {
				return true
			}
			for i := range size {
				b := buf.Get(max + 1)
				w.buffers[i] = b
				w.iovs[i].Base = unsafe.SliceData(b.FreeBytes())
				w.iovs[i].SetLen(max + 1)
				w.msgs[i] = darwinMessage{
					name:    (*byte)(unsafe.Pointer(&w.names[i])),
					nameLen: uint32(unsafe.Sizeof(w.names[i])),
					iov:     &w.iovs[i],
					iovLen:  1,
				}
			}
		}
		for {
			n, _, e := syscall.Syscall6(recvmsgX, fd, uintptr(unsafe.Pointer(&w.msgs[0])), uintptr(size), syscall.MSG_DONTWAIT, 0, 0)
			errno = e
			if e == syscall.EINTR {
				continue
			}
			if e == syscall.EAGAIN {
				return false
			}
			if e == 0 {
				count = int(n)
			}
			return true
		}
	})
	if err != nil {
		return 0, err, true
	}
	if probeErr != nil {
		return 0, os.NewSyscallError("recvfrom", probeErr), true
	}
	if unsupportedDarwin(errno) {
		return 0, nil, false
	}
	if errno != 0 {
		return 0, os.NewSyscallError("recvmsg_x", errno), true
	}
	if count == 0 {
		return 0, syscall.EIO, true
	}
	n := 0
	for i := range count {
		m := &w.msgs[i]
		if m.dataLen > uint64(max) || m.flags&syscall.MSG_TRUNC != 0 {
			err = errors.Join(err, ErrTruncated)
			continue
		}
		addr, addrErr := batchPeer(c, &w.names[i])
		if addrErr != nil {
			err = errors.Join(err, addrErr)
			continue
		}
		b := w.buffers[i]
		w.buffers[i] = nil
		dst[n] = finishPacket(b, int(m.dataLen), max, addr)
		n++
	}
	return n, err, true
}

func (w *darwinWorkspace) release() {
	for _, b := range w.buffers {
		if b != nil {
			b.Release()
		}
	}
	*w = darwinWorkspace{}
	darwinPool.Put(w)
}

func unsupportedDarwin(e syscall.Errno) bool {
	return e == syscall.ENOSYS || e == syscall.EOPNOTSUPP || e == syscall.ENOTSUP
}

func writeBatch(c *net.UDPConn, packets []Packet) (int, error, bool) {
	// The extended send operation supports connected, nonempty datagrams.
	if c.RemoteAddr() == nil {
		return 0, nil, false
	}
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, err, true
	}
	w := darwinPool.Get().(*darwinWorkspace)
	defer w.release()
	total := 0
	for total < len(packets) {
		batch := packets[total : total+min(darwinBatchLimit, len(packets)-total)]
		for i, p := range batch {
			data := p.Buffer.Bytes()
			if len(data) == 0 {
				batch = batch[:i]
				break
			}
			v := &w.iovs[i]
			v.Base = unsafe.SliceData(data)
			v.SetLen(len(data))
			w.msgs[i] = darwinMessage{iov: v, iovLen: 1}
		}
		if len(batch) == 0 {
			if err := WritePacket(c, packets[total]); err != nil {
				return total, err, true
			}
			total++
			continue
		}
		count := 0
		var errno syscall.Errno
		err = raw.Write(func(fd uintptr) bool {
			for count < len(batch) {
				n, _, e := syscall.Syscall6(sendmsgX, fd, uintptr(unsafe.Pointer(&w.msgs[count])), uintptr(len(batch)-count), 0, 0, 0)
				errno = e
				switch e {
				case syscall.EINTR:
					continue
				case syscall.EAGAIN:
					return false
				case 0:
					if n == 0 {
						errno = syscall.EIO
						return true
					}
					count += int(n)
				default:
					return true
				}
			}
			return true
		})
		runtime.KeepAlive(batch)
		total += count
		if err != nil {
			return total, err, true
		}
		if unsupportedDarwin(errno) {
			for total < len(packets) {
				if err := WritePacket(c, packets[total]); err != nil {
					return total, err, true
				}
				total++
			}
			return total, nil, true
		}
		if errno != 0 {
			return total, os.NewSyscallError("sendmsg_x", errno), true
		}
	}
	return total, nil, true
}
