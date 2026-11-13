package buf

import "testing"

func TestWrapReadableAndWritable(t *testing.T) {
	data := []byte("hello")
	readable := Wrap(data)
	if got := string(readable.Bytes()); got != "hello" {
		t.Fatalf("Wrap: got %q", got)
	}
	readable.Release()
	if string(data) != "hello" {
		t.Fatal("Release modified borrowed bytes")
	}
	writable := WrapWritable(data)
	if writable.Len() != 0 || len(writable.FreeBytes()) != len(data) {
		t.Fatalf("WrapWritable: Len=%d free=%d", writable.Len(), len(writable.FreeBytes()))
	}
	copy(writable.Extend(3), "abc")
	if string(writable.Bytes()) != "abc" {
		t.Fatalf("writable data = %q", writable.Bytes())
	}
	writable.Release()
}
