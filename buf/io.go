package buf

import (
	"errors"
	"io"
)

var errInvalidCount = errors.New("buf: invalid I/O byte count")

// Write appends as much of p as fits and returns io.ErrShortBuffer for a
// partial write. An empty p succeeds even when the buffer is full.
func (b *Buf) Write(p []byte) (int, error) {
	b.checkLive()
	n := copy(b.FreeBytes(), p)
	b.end += n
	if n < len(p) {
		return n, io.ErrShortBuffer
	}
	return n, nil
}

// WriteByte appends c or returns io.ErrShortBuffer if the buffer is full.
func (b *Buf) WriteByte(c byte) error {
	b.checkLive()
	if b.end == b.size {
		return io.ErrShortBuffer
	}
	b.data[b.end] = c
	b.end++
	return nil
}

// Read consumes up to len(p) bytes. It returns io.EOF for a nonempty p
// only when the buffer is empty.
func (b *Buf) Read(p []byte) (int, error) {
	b.checkLive()
	if len(p) == 0 {
		return 0, nil
	}
	if b.start == b.end {
		return 0, io.EOF
	}
	n := copy(p, b.Bytes())
	b.start += n
	return n, nil
}

// ReadByte consumes one byte, or returns io.EOF if the buffer is empty.
func (b *Buf) ReadByte() (byte, error) {
	b.checkLive()
	if b.start == b.end {
		return 0, io.EOF
	}
	c := b.data[b.start]
	b.start++
	return c, nil
}

// ReadBytes consumes exactly n bytes and returns a borrowed slice. On
// insufficient data it returns io.EOF for an empty buffer or
// io.ErrUnexpectedEOF otherwise, leaving the cursor unchanged. Negative n
// panics. A request for zero bytes succeeds.
func (b *Buf) ReadBytes(n int) ([]byte, error) {
	b.checkLive()
	if n < 0 {
		panic("buf: negative read size")
	}
	if n > b.Len() {
		if b.Len() == 0 {
			return nil, io.EOF
		}
		return nil, io.ErrUnexpectedEOF
	}
	start := b.start
	b.start += n
	return b.data[start:b.start:b.start], nil
}

// ReadOnceFrom appends the result of one Read call, including bytes returned
// with an error. If no space remains it returns io.ErrShortBuffer.
func (b *Buf) ReadOnceFrom(r io.Reader) (int, error) {
	b.checkLive()
	if b.end == b.size {
		return 0, io.ErrShortBuffer
	}
	return b.readFrom(r, b.size-b.end)
}

// ReadFullFrom appends exactly n bytes, retaining partial data on error.
// An oversized request returns io.ErrShortBuffer before reading. Negative n
// panics and zero succeeds without calling r.
func (b *Buf) ReadFullFrom(r io.Reader, n int) (int, error) {
	b.checkLive()
	if n < 0 {
		panic("buf: negative read size")
	}
	if n > b.size-b.end {
		return 0, io.ErrShortBuffer
	}
	total, empty := 0, 0
	for total < n {
		count, err := b.readFrom(r, n-total)
		total += count
		if total == n {
			return total, nil
		}
		if err != nil {
			if err == io.EOF && total != 0 {
				err = io.ErrUnexpectedEOF
			}
			return total, err
		}
		if count != 0 {
			empty = 0
		} else {
			empty++
			if empty == 100 {
				return total, io.ErrNoProgress
			}
		}
	}
	return total, nil
}

func (b *Buf) readFrom(r io.Reader, limit int) (int, error) {
	end := b.end + limit
	n, err := r.Read(b.data[b.end:end:end])
	if n < 0 || n > limit {
		return 0, errInvalidCount
	}
	b.end += n
	return n, err
}

// WriteTo writes unread bytes and consumes only those accepted by w. A short
// write without an error returns io.ErrShortWrite. An empty buffer makes no
// call to w.
func (b *Buf) WriteTo(w io.Writer) (int64, error) {
	b.checkLive()
	if b.start == b.end {
		return 0, nil
	}
	p := b.Bytes()
	n, err := w.Write(p)
	if n < 0 || n > len(p) {
		return 0, errInvalidCount
	}
	b.start += n
	if n < len(p) && err == nil {
		err = io.ErrShortWrite
	}
	return int64(n), err
}
