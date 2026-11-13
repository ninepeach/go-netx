//go:build !(linux && (amd64 || arm64))

package transfer

import (
	"io"
	"runtime"

	"github.com/ninepeach/go-netx/bufio"
)

// canWait reports whether bufio can read a TCP connection without holding
// a buffer while it is idle.
const canWait = runtime.GOOS == "darwin" || runtime.GOOS == "linux"

func kernelCopy(io.Writer) bool { return false }

func splice(io.Writer, io.Reader, []bufio.Counter, []bufio.Counter) (int64, error, bool) {
	return 0, nil, false
}
