//go:build linux && (amd64 || arm64)

package transfer

import (
	"io"
	"os"
	"syscall"

	"github.com/ninepeach/go-netx/bufio"
)

const (
	canWait = true

	spliceMove     = 0x1 // SPLICE_F_MOVE
	spliceNonblock = 0x2 // SPLICE_F_NONBLOCK
	spliceSize     = 64 << 10
)

// kernelCopy reports whether the standard library can move a TCP source to
// dst inside the kernel: to a stream socket, or to a file.
func kernelCopy(dst io.Writer) bool {
	_, file := dst.(*os.File)
	return file || streamSocket(dst)
}

// splice moves src to dst through a pipe without copying to user space,
// crediting rc with bytes taken from src and wc with bytes given to dst.
// ok is false only when nothing was moved and the caller may use another
// path: the endpoints are not bare stream sockets, or the kernel refused
// the first splice. Once a byte has moved, every outcome is final.
func splice(dst io.Writer, src io.Reader, rc, wc []bufio.Counter) (total int64, err error, ok bool) {
	if !streamSocket(dst) || !streamSocket(src) {
		return 0, nil, false
	}
	srcRaw, err := src.(syscall.Conn).SyscallConn()
	if err != nil {
		return 0, nil, false
	}
	dstRaw, err := dst.(syscall.Conn).SyscallConn()
	if err != nil {
		return 0, nil, false
	}
	var pipe [2]int
	if err := syscall.Pipe2(pipe[:], syscall.O_CLOEXEC|syscall.O_NONBLOCK); err != nil {
		return 0, nil, false
	}
	defer syscall.Close(pipe[0])
	defer syscall.Close(pipe[1])

	var (
		in, out  int64 // bytes of the current chunk: into the pipe, still in it
		sysErr   error
		readFunc = func(fd uintptr) bool {
			for {
				in, sysErr = syscall.Splice(int(fd), nil, pipe[1], nil, spliceSize, spliceNonblock)
				if sysErr != syscall.EINTR {
					break
				}
			}
			// The pipe is empty here, so EAGAIN means the socket has no data.
			return sysErr != syscall.EAGAIN
		}
		writeFunc = func(fd uintptr) bool {
			sysErr = nil // a retry after EAGAIN starts clean
			for out > 0 {
				n, err := syscall.Splice(pipe[0], nil, int(fd), nil, int(out), spliceNonblock|spliceMove)
				if err == syscall.EINTR {
					continue
				}
				if err != nil {
					sysErr = err
					// The pipe holds data, so EAGAIN means the socket is full.
					return err != syscall.EAGAIN
				}
				if n <= 0 {
					sysErr = io.ErrShortWrite
					return true
				}
				out -= n
				total += n
				count(wc, n)
			}
			return true
		}
	)
	for {
		in, sysErr = 0, nil
		if err := srcRaw.Read(readFunc); err != nil {
			return total, err, true // deadline or closed
		}
		if sysErr != nil {
			if total == 0 && (sysErr == syscall.EINVAL || sysErr == syscall.ENOSYS) {
				return 0, nil, false
			}
			return total, os.NewSyscallError("splice", sysErr), true
		}
		if in <= 0 {
			return total, nil, true // EOF
		}
		count(rc, in)
		out, sysErr = in, nil
		if err := dstRaw.Write(writeFunc); err != nil {
			return total, err, true
		}
		if sysErr != nil {
			if sysErr == io.ErrShortWrite {
				return total, sysErr, true
			}
			return total, os.NewSyscallError("splice", sysErr), true
		}
	}
}
