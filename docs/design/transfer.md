# transfer — target design specification

## Scope and status

Top-level `transfer` owns protocol-neutral stream and datagram forwarding orchestration. Stream copy, single-datagram `CopyPacket`, batch `CopyPackets`, and Linux amd64/arm64 splice are implemented. Vectorised multi-buffer read-wait is not implemented. It composes `bufio` capability interfaces, `buf` owned buffers and standard Go I/O interfaces. It does not own socket creation, TCP/UDP transport implementations, application framing, authentication, routing decisions, NAT tables or admission policy.

## First-round scope

Implemented: synchronous ordinary and extended stream copy, ordered cached-prefix replay, stepwise copy, bidirectional half-close, packet copy/batching, vectorised writes, TCP read-wait, and eligible Linux splice. Not implemented: multi-buffer vectorised read-wait paired with vectorised writes. Linux splice has not been runtime-verified on Linux.

## Implemented stream API

- `Copy` honors `io.WriterTo` first, then explicit extended or read-wait capabilities, then `io.ReaderFrom`. Ordinary streams use checked pooled copy. `CopyBuffered` explicitly selects the portable path.
- `NewCopySession` moves one chunk per `Step`. `SetEndpoints` permits an explicit capability refresh between steps and keeps counters and terminal errors.
- `CopyConn` reports both directional byte counts. Successful EOF half-closes the opposite writer. If half-close is unavailable, reverse copying continues with the destination open. A copy error or a supported half-close failure closes both endpoints and is returned as the failure reason. Context cancellation also closes both endpoints. Both workers finish before return. The error states what happened to the endpoints: nil means `CopyConn` closed neither, non-nil means it closed both. Cancellation arriving as both directions finish may produce either outcome, each of them whole: nil with both open, or `ctx.Err()` with both closed. Counts report the bytes copied in either case.

## Dependency rule

```text
application → transfer → bufio → buf
                         ↘ buf (directly where needed)
udp → bufio/buf (where required)
```

`bufio` and `udp` must not import `transfer`; `transfer` must not depend on concrete `tcp` or `udp` types. Transport-specific optimizations are discovered through narrow optional interfaces. Root `bufio` adapters must not invoke `transfer.Copy` from `ReadFrom` or `WriteTo`, to avoid cycles.

## Intended public capabilities (names provisional)

- `Copy(dst io.Writer, src io.Reader)`: ordered stream copy, including cached prefix replay, fallback and accurate accepted-byte accounting.
- `CopyConn(ctx context.Context, a, b net.Conn)`: bidirectional copy with cancellation and half-close. `CopyConnWithObserver` also reports per-direction bytes, copy errors and half-close outcomes. `CopyConnWithOptions` can bound the wait when half-close is unavailable.
- `CopyPacket(dst bufio.PacketWriter, src bufio.PacketReader, target netip.AddrPort) (source netip.AddrPort, err error)`: moves one datagram to a caller-chosen target without copying its payload, returns the datagram's source also when the write fails, and releases the datagram on every result. An invalid target is `ErrInvalidTarget` and nothing is read. A reader that returns neither a buffer nor an error yields `ErrInvalidPacket` and nothing is written. `CopyPacket` owns the datagram from the read until it returns; the writer only borrows it, and a writer that releases it is caught by `buf`'s double-release panic. The caller loops and chooses each target; `CopyPackets` provides bounded batch acceleration when both endpoints advertise compatible batch interfaces.
- `CopySession`: stepwise transfer with counters and explicit `SetEndpoints` capability refresh; it does not automatically detect handshake completion or provide transfer callbacks.

Existing exported signatures are authoritative in source. Future capability names remain provisional. No duplicate packet copy engine is placed in `udp`.

## Stream path selection

1. Drain or transfer all cached/pre-read bytes before accessing the underlying stream.
2. Discover optional direct-copy capability only through wrappers that explicitly permit replacement; do not bypass transformations, limits or counters.
3. Use Linux splice only for eligible plain stream endpoints, with poller-safe nonblocking behavior and precise pipe-byte accounting.
4. Otherwise select read-wait/vectorised or extended buffered copy, then ordinary checked copy as fallback.
5. Re-evaluate capabilities after handshake transitions and cached-buffer exhaustion; never replay already accepted bytes.

## Packet path selection

Preserve complete datagram boundaries and peer addresses. Prefer batch read/write when both ends advertise compatible capabilities; otherwise fall back to per-packet operations. Handle partial batch submission per packet; never resend a datagram whose delivery is ambiguous. Bind/NAT association and peer validation are provided by `udp`, not reimplemented by `transfer`.

## Ownership and accounting

Every pooled buffer has one owner and is released once. Buffer-taking APIs must state ownership on success and failure; no reference counting, `Reserve` or `OverCap`. Count only bytes confirmed accepted by the destination; read and write counters must distinguish their semantics when partial writes occur. A fast-path failure may fall back only at a proven safe offset, with pending splice-pipe bytes handled before switching. No hidden retry after ambiguous results.

## Concurrency, EOF and cancellation

One directional copy does not implicitly close the reverse direction. Where available, EOF can trigger `CloseWrite` on the peer while allowing reverse traffic to finish; unsupported half-close must not silently become full close. Context cancellation must unblock both workers by documented deadline/close behavior. Avoid goroutine leaks, concurrent ownership and deadlocks.

## Platform policy

Linux amd64/arm64: buffered, read-wait, vectorised and eligible splice; macOS arm64: supported native I/O and buffered fallback; Windows: compile and correct fallback, native paths only after validation. FreeBSD is not a current support target. Platform-specific implementations use build tags/internal helpers and must not leak into public interfaces.

## Acceptance and benchmarks

- Stream: cached-prefix preservation, short writes, EOF, half-close, cancellation, handshake upgrade, wrapper bypass prevention and exact counters.
- Direct/splice: partial transfer, EAGAIN/EINTR, pipe drain, unsupported fallback, close races and no duplicate bytes.
- Packet: zero-length/oversized datagrams, boundaries, addresses, batch partial success, ownership, counters and drop/error policy.
- Race/fuzz: concurrent close, cancellation, ownership transitions and partial I/O.
- Benchmark buffered vs read-wait/vectorised/splice, and single vs batch UDP, measuring throughput, allocations, CPU and syscalls before enabling optimizations by default.

## Implementation stages

1. Stabilize `bufio` capability interfaces, cache ownership and fallback semantics.
2. Implement correct ordinary/extended stream Copy, CopySession and bidirectional relay.
3. Implemented: TCP read-wait, vectorised writes, counters and eligible Linux splice; multi-buffer vectorised read-wait remains a separate enhancement.
4. Implemented: `CopyPacket` and optional `CopyPackets` batch dispatch; `udp` and `bufio` share one packet type and packet interfaces.
5. Verify cross-platform tests and performance; document any unsupported fast path.

## Source-aligned API and edge cases (Implemented)

Counting wrappers (`bufio.CountedReader`, `bufio.CountedWriter`) and explicitly authorized pass-through wrappers are unwrapped by `Copy` and `CopySession`; counters are credited for bytes actually read and accepted by the destination separately. `Copy` picks the first path that applies: splice for counted stream sockets on Linux amd64/arm64; the source's own `WriteTo` for uncounted endpoints (for a bare TCP source only where the standard library moves the data in the kernel); a pooled chunk at a time for a bare TCP source, any buffer or read-wait capability, or any counted endpoint, holding no buffer while a TCP source is idle; then the destination's `ReadFrom` and a buffered loop. Splice falls back only if the kernel refuses before any byte has moved. `CopyPackets(dst, src, target, scratch)` moves a batch when both ends offer `bufio.PacketBatchReader` and `bufio.PacketBatchWriter`, and one datagram otherwise; after a partial batch write the remainder is dropped, not resent. The splice path has been compiled and vetted for Linux but not executed there.

Current exports include `Copy`, `CopyBuffered`, `CopyConn`, `CopyConnWithObserver`, `CopyConnWithOptions`, `CopyConnOptions`, `Counts`, `NewCopySession`, `(*CopySession).Step`, `SetEndpoints`, `Transferred`, `CopyPacket`, and `CopyPackets`. Splice is an internal Linux-specific optimization, not an exported function.

`Copy` first discovers authorized wrappers and counters. It attempts Linux splice for eligible counted stream sockets; otherwise it considers an uncounted source's `io.WriterTo` (with special handling for bare TCP), then a pooled `CopySession` for counted/TCP/read-wait/extended I/O, then `io.ReaderFrom`, and finally `CopyBuffered`. Multi-buffer vectorised read-wait is not part of this dispatch. `CopyBuffered` uses 32 KiB, rejects out-of-range read/write counts with `bufio.ErrInvalidCount`, returns `io.ErrShortWrite` for a short nil-error write, and `io.ErrNoProgress` after 100 consecutive empty reads. The byte count is destination-accepted bytes.

`NewCopySession` panics for size <= 0. Steps must be serialized. A Step forwards a nonempty chunk even when the read reports an error, and terminal errors persist across later calls and `SetEndpoints`. Each step releases its buffer; neither session nor Copy closes borrowed endpoints.

`CopyConn` attempts destination `CloseWrite() error` after normal EOF. A missing method or `errors.ErrUnsupported` leaves the destination open until the reverse direction completes or the context is cancelled. Callers can set a positive `CopyConnOptions.HalfCloseTimeout` to close both connections, join both copies and return `ErrHalfCloseTimeout` if that wait expires. Error/cancellation closes both connections and waits for both copy directions. Success leaves both connections open. See [lifecycle](lifecycle.md) for precise ownership and cancellation contracts.
