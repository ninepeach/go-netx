//go:build !windows

package udp

func oversized(error) bool { return false }
