package bufio

import (
	"bytes"
	"io"
	"testing"
)

func TestDirectionalViews(t *testing.T) {
	var b bytes.Buffer
	r, w := ReadOnly{&b}, WriteOnly{&b}
	if _, ok := any(r).(io.WriterTo); ok {
		t.Fatal("read view exposes WriterTo")
	}
	if _, ok := any(w).(io.ReaderFrom); ok {
		t.Fatal("write view exposes ReaderFrom")
	}
	if _, err := w.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	p, err := io.ReadAll(r)
	if err != nil || string(p) != "x" {
		t.Fatalf("read = %q, %v", p, err)
	}
}
