//go:build darwin || (linux && !amd64)

package socket

import "syscall"

const soReusePort = syscall.SO_REUSEPORT
