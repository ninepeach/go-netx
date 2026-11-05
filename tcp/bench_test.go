package tcp_test

import (
	"context"
	"net"
	"testing"

	"github.com/ninepeach/go-netx/tcp"
)

// Accept throughput: one connect, accept, handler run and close per op.
//
// Checks: NF-13
func BenchmarkAccept(b *testing.B) {
	s, err := tcp.Listen(context.Background(), "tcp", "127.0.0.1:0", func(context.Context, *tcp.Conn) {}, tcp.Options{})
	if err != nil {
		b.Fatal(err)
	}
	go s.Serve(context.Background())
	defer s.Close()
	addr := s.Addr().String()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			b.Fatal(err)
		}
		c.Read(make([]byte, 1)) // returns when the server closes
		c.Close()
	}
	b.StopTimer()
	if st := s.Stats(); st.Accepted < uint64(b.N) {
		b.Fatalf("accepted %d of %d", st.Accepted, b.N)
	}
}
