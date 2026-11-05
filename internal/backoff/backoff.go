// Package backoff holds the retry delay shared by the transports.
package backoff

import "time"

const (
	Min = 5 * time.Millisecond
	Max = time.Second
)

// Next returns the delay after d: Min first, then doubling, capped at Max.
func Next(d time.Duration) time.Duration {
	return min(max(d*2, Min), Max)
}

// Wait sleeps for d unless stop is closed first, and reports whether it
// slept the full delay (false means stop fired).
func Wait(d time.Duration, stop <-chan struct{}) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-stop:
		return false
	}
}
