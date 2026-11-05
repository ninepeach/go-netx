# Package udp — design

Overall design: [ARCHITECTURE.md](../../ARCHITECTURE.md).

## 1. Scope

| Owns | Does not own |
|---|---|
| Binding, reader loops, inline dispatch | Reliability, ordering, sessions |
| Datagram size limits in both directions | Per-flow state (see `flow`) |
| Kernel drop and truncation accounting | Queuing work for later (see `queue`) |

Imports: `socket`, `internal/sys`, `internal/backoff`, `internal/counter`.

## 2. API

```go
func Listen(ctx, network, address string, h Handler, o Options) (*Conn, error)
type Handler func(c *Conn, p *Packet) // runs inline on the reader goroutine
type Packet struct {
    Data   []byte         // borrowed: valid until the handler returns
    From   netip.AddrPort // IPv4-mapped addresses unmapped
    Reader int            // reader index; flow-sticky with ReusePort
}
type Options struct {
    Readers   int // default 1; with ReusePort, one socket each
    MaxPacket int // receive buffer per reader; default and max MaxDatagram
    Socket    socket.Options
}
const MaxDatagram = 65000 // size limit for sends and receives
func (*Conn) Serve(context.Context) error
func (*Conn) Shutdown(context.Context) error
func (*Conn) Close() error
func (*Conn) WriteTo(b []byte, to netip.AddrPort) error
func (*Conn) Reply(p *Packet, b []byte) error
func (*Conn) LocalAddr() netip.AddrPort
func (*Conn) Stats() Stats
var ErrClosed, ErrTooLarge, ErrNilPacket
```

## 3. Design

**No goroutine, allocation or queue per datagram.** Each reader goroutine
owns one `MaxPacket` buffer, allocated once, and calls the handler inline.
`Packet` is reused; `From` uses `netip.AddrPort`, which needs no allocation.
Concurrency comes from `Readers`, not from goroutines per packet.

**Readers and sockets.** With `ReusePort`, `Listen` binds one socket per
reader to the same resolved port (`:0` works); the kernel hashes each
4-tuple to one socket, so a flow always reaches the same reader and a
gateway can shard per-flow state by `Packet.Reader` without locks. Without
it, readers share one socket.

**The kernel buffer is the queue.** A slow handler stops its reader; the
socket's receive queue (`socket.Options.RecvBuffer`) fills and the kernel
drops. On Linux `SO_RXQ_OVFL` reports the cumulative drop count with each
datagram; `KernelDrops` accumulates it wrap-safe across the 32-bit counter
and ignores stale values from readers sharing a socket. The kernel attaches
the count to the next queued datagram, so it lags once traffic stops.

**Truncation.** A datagram larger than `MaxPacket` is dropped and counted,
never delivered cut short: Linux reports `MSG_TRUNC`, Windows a
`WSAEMSGSIZE` read error; both become `Truncated`.

**Read errors** (`internal/sys.ClassifyRead`): transient errors (resource
pressure, ICMP errors) back off 5ms→1s, interrupted by Close; truncation is
counted; a fatal error closes the Conn and is returned by `Serve`.

**Sending.** `WriteTo` spreads writes over sockets by destination port;
`Reply` uses the socket that received the packet. Payloads over
`MaxDatagram` return `ErrTooLarge`. A nil packet or a foreign `Reader`
index never panics.

## 4. Concurrency and handler contract

Handlers must finish in bounded time and must not block on anything they
cannot bound: Shutdown and Close cannot interrupt a running handler, and a
handler that never returns keeps `Serve` from returning. There is no
context by design (D-03). Waiting work belongs on a gateway goroutine fed
through a bounded queue. Panics are not recovered.

## 5. Observability

`Stats()`: RecvPackets/Bytes, SendPackets/Bytes, Truncated, RecvErrors,
SendErrors, KernelDrops, KernelDropsEnabled (false means "not measured",
not "no drops"). Counters are padded per reader and per socket.

## 6. Performance (2 vCPU, loopback, 512B)

Echo round trip about 10–11µs with 0 allocations (`BenchmarkEcho`), against
32.5µs and 5 allocations in v0.2.

## 7. Decisions

D-01 (`Readers` not capped), D-02 (one limit, 65000, both directions), D-03
(no handler context).

## 8. Testing

UDP-01…UDP-31, OBS-03…OBS-05, NF-04/08/12 in
[docs/TESTING.md](../TESTING.md). UDP-20 (IPv6) is written but has not
run on an IPv6 host yet.

## 9. Open items

- recvmmsg/sendmmsg batching; the API does not change.
