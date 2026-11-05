package sys

import (
	"net"
	"net/netip"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func batchSetup(t *testing.T, batch, size int) (*net.UDPConn, *net.UDPConn, *BatchReader, [][]byte) {
	t.Helper()
	rc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rc.Close() })
	raw, _ := rc.SyscallConn()
	if !EnableDropCount(raw) {
		t.Fatal("SO_RXQ_OVFL not enabled")
	}
	sc, err := net.DialUDP("udp4", nil, rc.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sc.Close() })
	bufs := make([][]byte, batch)
	for i := range bufs {
		bufs[i] = make([]byte, size)
	}
	return rc, sc, NewBatchReader(raw, bufs, OOBSize), bufs
}

// Checks: SYS-05
func TestBatchReaderReadsQueuedDatagrams(t *testing.T) {
	rc, sc, br, bufs := batchSetup(t, 8, 100)
	rc.SetReadDeadline(time.Now().Add(2 * time.Second))
	// 5 queued datagrams: one call returns all of them, without waiting
	// for the batch to fill; the 4th is larger than its buffer.
	sizes := []int{0, 10, 100, 150, 7}
	for i, n := range sizes {
		b := make([]byte, n)
		for j := range b {
			b[j] = byte(i)
		}
		if _, err := sc.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	n, err := br.Read()
	if err != nil || n != len(sizes) {
		t.Fatalf("Read = %d, %v; want %d datagrams in one call", n, err, len(sizes))
	}
	want := sc.LocalAddr().(*net.UDPAddr).AddrPort()
	for i, size := range sizes {
		got, oob, flags, from := br.Message(i)
		if from != want {
			t.Fatalf("datagram %d from %v, want %v", i, from, want)
		}
		if _, ok := DropCount(oob); ok {
			t.Fatalf("datagram %d: drop count without drops", i)
		}
		if size > 100 {
			if flags&MsgTrunc == 0 || got != 100 {
				t.Fatalf("datagram %d: len %d flags %#x, want truncated to 100", i, got, flags)
			}
			continue
		}
		if flags&MsgTrunc != 0 || got != size {
			t.Fatalf("datagram %d: len %d flags %#x, want %d untruncated", i, got, flags, size)
		}
		for _, c := range bufs[i][:got] {
			if c != byte(i) {
				t.Fatalf("datagram %d: wrong contents %v", i, bufs[i][:got])
			}
		}
	}

	// A full batch: 20 queued, read 8 + 8 + 4.
	for i := 0; i < 20; i++ {
		sc.Write([]byte{byte(i)})
	}
	var counts []int
	for total := 0; total < 20; {
		n, err := br.Read()
		if err != nil {
			t.Fatal(err)
		}
		counts = append(counts, n)
		for i := 0; i < n; i++ {
			if bufs[i][0] != byte(total+i) {
				t.Fatalf("order: got %d at %d", bufs[i][0], total+i)
			}
		}
		total += n
	}
	if len(counts) != 3 || counts[0] != 8 || counts[1] != 8 || counts[2] != 4 {
		t.Fatalf("batches %v, want [8 8 4]", counts)
	}

	// Overflow the queue: the next datagram queued carries the kernel's
	// drop count in its control data.
	rc.SetReadBuffer(1)
	for i := 0; i < 200; i++ {
		sc.Write(make([]byte, 500))
	}
	drain(t, rc, br)
	rc.SetReadBuffer(1 << 20)
	sc.Write([]byte{1})
	if n, err := br.Read(); err != nil || n != 1 {
		t.Fatalf("Read = %d, %v", n, err)
	}
	if _, oob, _, _ := br.Message(0); true {
		if d, ok := DropCount(oob); !ok || d == 0 {
			t.Fatalf("drop count %d, %v; want > 0 after overflow", d, ok)
		}
	}

	// Close wakes a blocked Read with net.ErrClosed.
	done := make(chan error, 1)
	rc.SetReadDeadline(time.Time{})
	go func() { _, err := br.Read(); done <- err }()
	time.Sleep(20 * time.Millisecond)
	rc.Close()
	select {
	case err := <-done:
		if ClassifyRead(err) != Fatal || !isClosed(err) {
			t.Fatalf("Read after Close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not wake Read")
	}
}

// drain reads until the queue is empty.
func drain(t *testing.T, rc *net.UDPConn, br *BatchReader) {
	t.Helper()
	for {
		rc.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		if _, err := br.Read(); err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				rc.SetReadDeadline(time.Now().Add(2 * time.Second))
				return
			}
			t.Fatal(err)
		}
	}
}

func isClosed(err error) bool {
	ne, ok := err.(*net.OpError)
	return ok && ne.Err != nil && ne.Err.Error() == net.ErrClosed.Error()
}

// Checks: SYS-05
func TestBatchReaderDoesNotAllocate(t *testing.T) {
	_, sc, br, _ := batchSetup(t, 8, 64)
	msg := make([]byte, 32)
	allocs := testing.AllocsPerRun(200, func() {
		for i := 0; i < 4; i++ {
			sc.Write(msg)
		}
		for got := 0; got < 4; {
			n, err := br.Read()
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < n; i++ {
				br.Message(i)
			}
			got += n
		}
	})
	if allocs > 0 {
		t.Fatalf("%.1f allocs per batch", allocs)
	}
}

// Checks: SYS-06
func TestBatchReaderAddressParsing(t *testing.T) {
	r := &BatchReader{}
	var sa syscall.RawSockaddrAny

	in4 := (*syscall.RawSockaddrInet4)(unsafe.Pointer(&sa))
	in4.Family = syscall.AF_INET
	in4.Addr = [4]byte{192, 0, 2, 1}
	*(*[2]byte)(unsafe.Pointer(&in4.Port)) = [2]byte{0x1f, 0x90} // 8080
	if got := r.addr(&sa); got != netip.MustParseAddrPort("192.0.2.1:8080") {
		t.Fatalf("IPv4: %v", got)
	}

	sa = syscall.RawSockaddrAny{}
	in6 := (*syscall.RawSockaddrInet6)(unsafe.Pointer(&sa))
	in6.Family = syscall.AF_INET6
	in6.Addr = netip.MustParseAddr("::ffff:192.0.2.1").As16()
	*(*[2]byte)(unsafe.Pointer(&in6.Port)) = [2]byte{0, 53}
	if got := r.addr(&sa); got != netip.MustParseAddrPort("192.0.2.1:53") {
		t.Fatalf("IPv4-mapped is not unmapped: %v", got)
	}

	in6.Addr = netip.MustParseAddr("fe80::1").As16()
	in6.Scope_id = 999999 // no such interface: the number, as net does
	if got := r.addr(&sa); got.Addr().Zone() != "999999" || got.Port() != 53 {
		t.Fatalf("link-local zone: %v", got)
	}
	if lo, err := net.InterfaceByName("lo"); err == nil {
		in6.Scope_id = uint32(lo.Index)
		if got := r.addr(&sa); got.Addr().Zone() != "lo" {
			t.Fatalf("zone for lo: %v", got)
		}
	}

	in6.Addr = netip.MustParseAddr("2001:db8::1").As16()
	in6.Scope_id = 0
	if got := r.addr(&sa); got != netip.MustParseAddrPort("[2001:db8::1]:53") {
		t.Fatalf("IPv6: %v", got)
	}

	sa = syscall.RawSockaddrAny{}
	if got := r.addr(&sa); got.IsValid() {
		t.Fatalf("unknown family: %v", got)
	}
}
