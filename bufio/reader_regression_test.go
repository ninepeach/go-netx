package bufio

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ninepeach/go-netx/buf"
)

type emptyThenData struct {
	empty int
	data  string
}

func (r *emptyThenData) Read(p []byte) (int, error) {
	if r.empty > 0 {
		r.empty--
		return 0, nil
	}
	if r.data == "" {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func TestReaderEmptyReadsThenData(t *testing.T) {
	for _, size := range []int{1, 1024} {
		r := NewReader(&emptyThenData{empty: 3, data: "ok"}, ReaderOptions{})
		p := make([]byte, size)
		n, err := r.Read(p)
		if err != nil || string(p[:n]) != "ok" && !(size == 1 && string(p[:n]) == "o") {
			t.Fatalf("size=%d n=%d err=%v data=%q", size, n, err, p[:n])
		}
		r.Release()
	}
}

func TestReaderNoProgressSticky(t *testing.T) {
	for _, size := range []int{1, 1024} {
		r := NewReader(&emptyThenData{empty: 101, data: "ok"}, ReaderOptions{})
		p := make([]byte, size)
		if _, err := r.Read(p); !errors.Is(err, io.ErrNoProgress) {
			t.Fatalf("size=%d first error=%v", size, err)
		}
		if _, err := r.Read(p); !errors.Is(err, io.ErrNoProgress) {
			t.Fatalf("size=%d sticky error=%v", size, err)
		}
		r.Release()
	}
}

func TestCachedReaderPrefixFirst(t *testing.T) {
	prefix := buf.Copy([]byte("pre"))
	r := NewCachedReader(strings.NewReader("fix"), prefix)
	defer r.Close()
	p := make([]byte, 3)
	n, err := r.Read(p)
	if err != nil || n != 3 || string(p) != "pre" {
		t.Fatalf("prefix n=%d err=%v data=%q", n, err, p)
	}
	n, err = r.Read(p)
	if err != nil || n != 3 || string(p) != "fix" {
		t.Fatalf("source n=%d err=%v data=%q", n, err, p)
	}
}
