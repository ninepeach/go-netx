package bufio

import "io"

// ReadOnly exposes the read direction and suppresses optional capabilities.
type ReadOnly struct{ io.Reader }

// WriteOnly exposes the write direction and suppresses optional capabilities.
type WriteOnly struct{ io.Writer }
