package transfer

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/ninepeach/go-netx/bufio"
	"github.com/ninepeach/go-netx/tcp"
)

type guardedTCP struct {
	*tcp.Conn
	writes bytes.Buffer
	reads  *strings.Reader
}

func (c *guardedTCP) Write(p []byte) (int, error) { return c.writes.Write(p) }
func (c *guardedTCP) Read(p []byte) (int, error)  { return c.reads.Read(p) }

func TestEmbeddingTCPConnDoesNotBypassOuterIO(t *testing.T) {
	raw, peer := tcpPair(t)
	if _, err := peer.Write([]byte("raw input")); err != nil {
		t.Fatal(err)
	}
	if err := peer.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	guard := &guardedTCP{Conn: &tcp.Conn{Conn: raw}, reads: strings.NewReader("filtered input")}
	vec := [][]byte{[]byte("ab"), []byte("cd")}
	for _, path := range []struct {
		name  string
		write func() (int64, error)
	}{
		{"WriteVectorised", func() (int64, error) { return bufio.WriteVectorised(guard, vec) }},
		{"Flush", func() (int64, error) {
			w := bufio.NewWriter(guard)
			for _, p := range vec {
				w.AddShared(p)
			}
			return w.Flush()
		}},
		{"Copy", func() (int64, error) { return Copy(guard, strings.NewReader("abcd")) }},
	} {
		t.Run(path.name, func(t *testing.T) {
			guard.writes.Reset()
			n, err := path.write()
			if n != 4 || err != nil || guard.writes.String() != "abcd" {
				t.Fatalf("write = (%d, %v), wrapper received %q", n, err, guard.writes.String())
			}
		})
	}
	var dst bytes.Buffer
	n, err := Copy(&dst, guard)
	if n != int64(len("filtered input")) || err != nil || dst.String() != "filtered input" {
		t.Fatalf("copy = (%d, %v), got %q", n, err, dst.String())
	}
	if err := raw.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if p, err := io.ReadAll(peer); err != nil || len(p) != 0 {
		t.Fatalf("wrapper bypassed: TCP peer received %q, %v", p, err)
	}
}
