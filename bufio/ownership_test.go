package bufio

import (
	"bytes"
	"net"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestPeekCannotAppendIntoUnreadData(t *testing.T) {
	r := NewReader(bytes.NewBufferString("abcd"), ReaderOptions{})
	p, err := r.Peek(2)
	if err != nil {
		t.Fatal(err)
	}
	if cap(p) != 2 {
		t.Fatalf("Peek capacity = %d", cap(p))
	}
	p = append(p, 'X')
	if string(r.Buffered()) != "abcd" {
		t.Fatalf("append overwrote buffered data: %q", r.Buffered())
	}
	r.Release()
	if r.src != nil {
		t.Fatal("released reader retained source")
	}
}

func TestWriterPreservesUnixDatagrams(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("requires Unix datagrams")
	}
	// Keep socket paths below the platform sockaddr_un limit.
	path := filepath.Join(t.TempDir(), "sock")
	addr := &net.UnixAddr{Name: path, Net: "unixgram"}
	server, err := net.ListenUnixgram("unixgram", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := net.DialUnix("unixgram", nil, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	deadline := time.Now().Add(3 * time.Second)
	if err := server.SetReadDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	if err := client.SetWriteDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	w := NewWriter(client)
	w.AddShared([]byte("first"))
	w.AddShared([]byte("second"))
	if n, err := w.Flush(); n != 11 || err != nil {
		t.Fatalf("Flush = %d, %v", n, err)
	}
	var p [32]byte
	for _, want := range []string{"first", "second"} {
		n, err := server.Read(p[:])
		if err != nil || string(p[:n]) != want {
			t.Fatalf("datagram = %q, %v, want %q", p[:n], err, want)
		}
	}
}
