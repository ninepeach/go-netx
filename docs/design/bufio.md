# bufio — design specification

## Choosing an API

Start with `NewReader`, `NewWriter`, and `ReadBuffer` / `WriteBuffer` for
buffered I/O. Use `transfer.Copy` or `transfer.CopyConn` to compose copying.
The extended I/O, vector, counting, and unwrapping interfaces are capabilities
for adapter and transport authors.

`ReaderUnwrapper` and `WriterUnwrapper` take the outer reader or writer as
an argument. A receiver must return nil if that argument is a different
object, including when Go promotes its method through embedding. A declined
unwrap leaves the outer I/O path in place.

## Scope

`bufio` provides buffered and extended stream/packet I/O, connection adapters, capability discovery, vectorised I/O interfaces, read-wait, packet I/O and batch capability adapters, and deadline behavior. It does **not** own copy sessions, zero-copy orchestration, NAT associations or transfer policy; those belong to [`transfer`](transfer.md) and `udp` as specified below. It remains protocol-neutral: framing, authentication, encryption, routing decisions and session policy are implemented by callers. This document specifies the intended module behavior; availability in the current source tree is tracked separately in [module status](README.md).

## Architecture and dependency boundary

```text
Application / protocol adapters
          |
      transfer  (Copy, CopySession, direct/splice, packet copy)
          |
        bufio   (Reader/Writer, capability interfaces, wrappers, wait, batch adapters)
          |
         buf    (owned memory)
```

`udp` provides datagram transport, Bind and peer address translation adapters; `transfer` depends on interfaces rather than concrete UDP implementations. `bufio` must never import `transfer`. Platform syscall implementations are isolated with build tags or internal helpers. Fast paths must preserve every wrapper's observable behavior; a wrapper exposes capabilities only when it preserves accounting, transformation, ordering and lifecycle semantics.

## Existing buffered stream API

| API | Contract |
|---|---|
| `NewReader(src, ReaderOptions{IdleSize, Max})` | Lazily allocate an adaptive reader; defaults 512B / 64KiB |
| `Reader.Peek`, `Fill`, `Buffered`, `Len` | Observe and fill unread bytes, bounded by `Max` |
| `Reader.Discard`, `Read`, `Shrink` | Consume, direct-read when appropriate, return idle storage |
| `Reader.TakeBuffered` | Transfer ownership of unread bytes for replay by a downstream consumer |
| `Reader.Release` | Idempotently end reader use |
| `NewWriter`, `Writer.Add`, `AddShared` | Gather owned buffers and borrowed immutable segments |
| `Writer.Pending`, `Flush`, `Discard` | Report pending data, write in order, or discard |
| `ErrTooLarge`, `ErrNegativeCount`, `ErrReleased`, `ErrInvalidCount` | Explicit bounds and lifecycle errors |

`Reader` retains bytes returned with a source error and reports the sticky error after buffered bytes are consumed. `Peek` returns available bytes alongside an error if it cannot satisfy the requested count. `Fill` performs at most one source read. One hundred consecutive `(0, nil)` reads return `io.ErrNoProgress`. `TakeBuffered` transfers the unread buffer without consuming the underlying stream; downstream copy must replay it first.

`Writer` preserves segment order. `Add` transfers buffer ownership; `AddShared` borrows an immutable slice until `Flush` or `Discard` returns. `Flush` releases all owned buffers and drops all shared references, including after partial writes. It reports the acknowledged byte count and error; unsent bytes are not retained for implicit retry. `Discard` releases without writing. Neither type is safe for concurrent method calls.

## Extended I/O and connection adapters

The module defines narrow capability contracts for buffer-based reads/writes, vectorised writes, read readiness, packet reads/writes and packet batching. Adapters preserve ordinary `io.Reader`, `io.Writer`, `net.Conn` and `net.PacketConn` behavior while exposing supported extended operations.

Connection adapters cover:
- Cached/pre-read connections: replay cached bytes before reading the underlying connection; close and read synchronization must not race or double-release.
- Read-only/write-only, address-preserving and append/prefix adapters: preserve method semantics and connection metadata.
- Counter connections and packet connections: account for actual transferred bytes, including fast paths and partial success.
- Deadline and close adapters: preserve read/write deadlines, independent half-close and idempotent cleanup.
- Fallback adapters: retain the same data, address and ownership behavior when an optimization is unavailable.
- Protocol adapters: declare required header/tail overhead and whether a destination remains an IP address or domain name. Wrapping must not silently discard address metadata.

A wrapper must not expose raw-socket bypass when it transforms data (TLS, compression, encryption), enforces rate limits, or requires per-byte inspection. A counter wrapper may expose an accelerated path only if counters remain exact.

## Transfer orchestration boundary

Stream copy, CopySession, bidirectional relay, handshake-sensitive path switching, direct copy and transfer counters belong to [`transfer`](transfer.md). `bufio` supplies the capability contracts and adapters they consume, including cached-byte replay and wrapper replaceability. Adapter `ReadFrom`/`WriteTo` implementations must not import `transfer`; they may use a checked standard-library fallback. No fast path may bypass pending cached data, TLS, compression, rate limiting or counters.

## First-round adapters

`ExtendedReader` appends to a borrowed buffer. `ExtendedWriter` borrows data and reports acknowledged bytes. `ExtendedConn` combines both with `net.Conn`. `ReadBuffer` returns an owned chunk whenever data arrived, including with an error. `WriteBuffer` validates progress and uses ordinary Write when the extension is absent. Packet capabilities use one owned buffer and canonical peer per datagram; a packet write only borrows.

`NewCachedReader` takes an owned prefix and borrows its source. `NewCachedConn` owns its prefix and closes the underlying connection. Both release unread cached data on close. `AppendConn` supplies optional directional I/O overrides. `WithAddresses` projects addresses while forwarding deadlines and half-close. An overridden I/O direction must honor the original connection's lifetime when used with cancellable transfer.

`NewReadWaiter` discovers explicit readiness or a bare TCP socket on Linux and macOS. The socket's read deadline and close govern the wait. Serialize a waiter with reads. Other endpoints use ordinary I/O.

`NewCounterConn` provides a connection adapter that counts bytes read and bytes accepted by writes, including partial progress. It forwards half-close and deadlines. Transfer results account separately for bytes delivered per direction.

## Vectorised I/O

A vectorised writer accepts ordered byte slices and reports total accepted bytes. Capability discovery first checks an explicitly exposed writer/creator interface, then supported socket facilities, then a safe ordinary-write fallback. Unix-like systems may use `writev` through a supported raw connection; Windows uses its supported socket write facilities. Segment limits, `EINTR`, `EAGAIN`, deadlines, partial iovec consumption and poller errors are handled without re-sending accepted bytes.

For destinations without a vectorised capability, use checked sequential writes or an explicitly bounded coalesced buffer where beneficial. Do not unwrap a connection solely to access its descriptor if doing so would bypass a wrapper. Batch storage is cleared after use; borrowed slices remain valid until the operation returns.

## Zero-copy capability boundary

The `transfer` package may use Linux `splice` for eligible plain stream-to-stream transfers through a nonblocking pipe and compatible raw socket descriptors. Eligibility requires no unread cached bytes, no payload transformation, compatible wrappers and reliable accounting. Handle pipe setup, partial transfers, readiness, `EAGAIN`, `EINTR`, EOF and descriptor cleanup.

When unsupported, choose buffered copy. If a fast path has already transferred bytes, fallback resumes at the exact remaining offset; pending pipe bytes must be drained or otherwise accounted for before switching. No retry may duplicate data. TLS, compression and other payload-transforming wrappers use their own supported paths rather than bypassing transformations. Non-Linux platforms provide a behaviorally equivalent buffered path.

## ReadWait and direct copy

ReadWait adapters expose readiness or a prepared buffer without creating an independent read loop that races the connection. Copy can use a reader's direct-transfer capability when it preserves cached bytes, ownership, deadlines and accounting. Every wait operation must have a cancellation/unblock strategy. Spurious readiness and temporary unavailability are not EOF.

## Packet I/O and UDP batching capabilities

`Packet{Buffer *buf.Buf; Addr netip.AddrPort}` is the one datagram type: the peer is the source when received and the destination when sent. `PacketReader.ReadPacket() (Packet, error)` hands the caller an owned buffer; `PacketWriter.WritePacket(Packet) error` borrows it. `udp` implements both and `transfer` composes them, so neither needs the other.

Packet operations preserve datagram boundaries, source/destination addresses and any required protocol metadata. A packet is either accepted as a whole or rejected; stream-style partial replay is not applied to datagrams. Oversized incoming packets are handled according to explicit drop/report policy without unintentionally terminating an otherwise usable UDP association.

Batch interfaces support multiple packets per operation with explicit per-packet ownership and result accounting. Supported platforms may use:
- Linux: `recvmmsg`/`sendmmsg` and eligible UDP offload capabilities.
- Darwin: supported batch receive/send facilities and `sendto`-based batching.
- Windows and other supported builds: checked per-packet fallback unless a native capability is available.

Offload features such as UDP segmentation/coalescing are enabled only when socket and platform capability checks succeed. Segment size, MTU, header overhead, destination consistency and per-packet error handling remain explicit. Unsupported offload must not alter packet contents or boundaries.

## Bind and peer translation boundary

`bufio` defines packet and batch I/O capability interfaces and low-level adapters. Datagram socket behavior, connected/unconnected Bind adapters, peer validation and peer address translation belong to `udp`; association tables, sessions and routing belong to the application, consistent with the [V1 UDP contract](udp.md). Stream/packet forwarding loops, packet batch copy strategy, transfer counters and fallback orchestration belong to `transfer`. Neither `udp` nor `bufio` imports `transfer`. Routing decisions, admission policy and unbounded association management are outside the library's low-level I/O contracts.

## Ownership, errors and concurrency

Each buffer-taking method documents whether it borrows or transfers ownership and who releases it after failure. Cached buffers are drained or transferred before direct copy. No `*buf.Buf` is simultaneously owned by two workers. Packet batch APIs define ownership separately for successfully submitted and unsubmitted packets. No automatic retry after an ambiguous partial write.

Read and write sides may run concurrently where the underlying connection supports it; mutable Reader/Writer state is serialized. Connection close must unblock waits and not race buffered-byte ownership. Invalid byte counts, short writes, timeouts, cancellations, unsupported capabilities and permanent transport failures are distinct. Only an optimization-specific unsupported condition triggers fallback; semantic failures propagate.

## Platform contract

| Capability | Linux | macOS | Windows |
|---|---|---|---|
| Buffered stream and packet I/O | Required | Required | Required |
| Vectorised stream writes | Native when supported | Native when supported | Native when supported; fallback |
| Stream splice zero-copy | Eligible sockets | Buffered fallback | Buffered fallback |
| Packet batching | Native mmsg when supported | Native batch/sendto when supported | Per-packet fallback |
| UDP offload | Capability-gated | Capability-gated where available | Capability-gated where available |
| I/O wrappers, capabilities and safe fallback | Required | Required | Required |

Linux and macOS are the primary execution-test platforms. Windows must compile and retain correct fallback behavior; its native paths require platform-specific validation. Unsupported platforms use build-tagged stubs where applicable, with no silent change to semantics.

## Acceptance and benchmarks

- Reader/Writer regression: bounds, sticky errors, no progress, buffer handoff, partial writes, cleanup, wrapper safety and race tests.
- Transfer acceptance (in [`transfer.md`](transfer.md)): cached prefix, bidirectional EOF, half-close, deadlines, cancellation, handshake transition, counter correctness and failure after partial transfer.
- Vectorised I/O: multi-iovec writes, short writes, invalid counts, `EINTR`/`EAGAIN`, deadline and wrapper fallback.
- Zero-copy: Linux splice success, unsupported fallback, pending pipe bytes, EOF, cancellation and exact counters.
- UDP: boundaries, truncation, oversize handling, batch partial success, offload fallback, address preservation, domain destination and buffer release.
- Bind and peer translation: unauthorized-source rejection, concurrent close and packet loss/duplication checks.
- `go test -race` for concurrency-sensitive paths; fuzz stream framing boundaries, packet sizes, ownership transitions and partial results.
- Benchmarks: throughput, allocations, CPU, syscalls and latency for TCP copy (buffered/vectorised/splice), 1/8/32-segment writes, UDP single/batch at small and MTU-sized datagrams, idle connection memory and multi-session contention. Every optimization is compared with its correct fallback.

## Implementation status

A connection type that adds nothing to I/O, such as `tcp.Conn`, declares `UnwrapReader(outer io.Reader) io.Reader` and `UnwrapWriter(outer io.Writer) io.Writer` (`ReaderUnwrapper`, `WriterUnwrapper`); the signatures use only `io` types, so the declaring package does not import `bufio`. `Writer.Flush`, `WriteVectorised`, `UnwrapCountReader` and `UnwrapCountWriter` step through such wrappers and through nothing else: TLS, limiters and buffering wrappers do not declare it and are never bypassed. `CounterConn` implements `CountedReader`, `CountedWriter` and `VectorisedWriter`, so a counter costs neither a batch its `writev` nor a transfer its fast path. `WriteVectorised(w, vec)` is usable on its own: the writer's capability, `writev` through `net.Buffers` on a bare stream connection, or one checked write per segment; it does not change `vec`. On Linux and macOS the TCP read-waiter reads through the descriptor in one system call and holds a buffer only when bytes are available.

The adaptive Reader, gathered Writer, stream and packet capability interfaces, cached and directional adapters, explicit vector writes and TCP read-wait are implemented. Packet batching and additional deadline adapters are later targets. See [module status](README.md).
