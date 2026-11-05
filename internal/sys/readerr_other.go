//go:build !unix && !windows

package sys

// ClassifyRead treats every read error as fatal on this platform.
func ClassifyRead(error) ReadErr { return Fatal }
