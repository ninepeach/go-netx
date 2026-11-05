//go:build !linux

package leak

func openFDs() int { return -1 }
