# go-netx architecture

go-netx is the shared network foundation for the IM, Game and Media
gateways. Each gateway owns its protocol, deployment, resource budget and
failure domain; go-netx owns only what all of them need and none of them
should reimplement.

**This layout is frozen.** Changes to it follow the rules at the end.

## Design principles

1. **Hot paths do not create goroutines, allocate or queue.** Anything that
   cannot meet this does not go into a transport.
2. **No business semantics.** No message types, sessions, rooms, users or
   authentication. If code needs to understand packet contents, it belongs
   in a gateway.
3. **Memory is preallocated when its count scales with cores, pooled when it
   scales with connections, and never allocated per packet.**
4. **Zero external dependencies.** Observability is exposed as snapshots,
   not tied to a monitoring product.

## Packages

| Package | Domain | Owns | Does not own |
|---|---|---|---|
| `netx` (root) | Runtime | Start, signals, three-phase shutdown, `Group` | Connections, protocols |
| `socket/` | Transport | Socket options, platform differences | Server lifecycles |
| `tcp/` | Transport | Listener, accept loop, `Conn` with ID, Dialer | Sessions, framing |
| `udp/` | Transport | Sockets, reader loops, datagram send/receive | Reliability, streams |
| `flow/` | Flow | Flow ID (doubles as epoch), close state machine, idle-expiring `Table` | I/O, authentication, user identity |
| `limit/` | Resource control | Admission (`Limiter`), totals (`Budget`) | Queues |
| `queue/` | Resource control | Bounded `Ring`, overflow policy, backpressure | Global budgets |
| `buf/` | Buffer management | `Pool`, size classes, ownership rules | Encoding |
| `internal/` | — | Syscalls, padded counters | Anything public |

Observability has no package: every component exposes `Stats()` returning a
value snapshot, and errors are exported sentinels per package (`udp.ErrClosed`,
`tcp.ErrClosed`, `queue.ErrClosed`, …), matched with `errors.Is`.

**Session is not here.** A flow is transport identity: one TCP connection or
one UDP source address, ending when the transport ends. A session is business
identity: a logged-in user that survives reconnects. Sessions belong to each
gateway.

## Dependency direction

```
netx (root)        → nothing
limit, queue, flow → nothing
buf                → limit
socket             → nothing
tcp                → socket, limit, internal
udp                → socket, internal
```

- Transports do not import `flow`, `queue` or `buf`. Gateways compose them.
- `tcp` and `udp` never import each other. The root never imports them;
  they satisfy `netx.Service` structurally.
- No cycles, ever.

## Runtime contract

```go
type Service interface {
    Serve(context.Context) error    // blocks until fully stopped; ctx cancel = abort
    Shutdown(context.Context) error // stop accepting → drain until ctx → abort
}
```

`netx.Server` runs one service (use `netx.Group` for several):

1. A stop signal (OS signal, parent ctx, `Stop`, `Shutdown`) starts the drain.
   It does **not** cancel the service context.
2. `Service.Shutdown` runs with `ShutdownTimeout`. TCP closes its listener and
   closes `Draining()`, so long-lived handlers can tell clients to move.
3. After Shutdown returns, the service context is cancelled (abort). Serve
   must return within `AbortGrace` or the server returns `ErrAbortTimeout`.

## Transport behaviour

**UDP.** `Options.Readers` goroutines each own one preallocated receive buffer
and call the handler inline. With `ReusePort` each reader has its own socket
and the kernel pins each flow to one reader (`Packet.Reader`), so gateways can
shard per-flow state without locks. There is no internal queue: the kernel
receive buffer is the queue (`socket.Options.RecvBuffer`), and overflows are
reported in `Stats.KernelDrops` on Linux. `udp.MaxDatagram` (65000) is the size limit in both directions;
`MaxPacket` defaults to it and may not exceed it.
Datagrams larger than `MaxPacket`
are dropped and counted, never delivered truncated.

**TCP.** One goroutine per connection on the Go netpoller. `Options.Admit`
decides admission at accept time; rejections are counted, not logged. The
handler context is cancelled only on abort.

## Ownership rules

1. **Borrowed** — data passed to a callback (`udp.Packet.Data`) is valid only
   until the callback returns. Never Release it; `buf.Pool.Copy` it to keep it.
2. **Owned** — a `*buf.Buf` from `Get`/`Copy` has exactly one owner, who
   Releases it exactly once.
3. **Transfer** — handing a `*buf.Buf` on (for example `queue.Ring.Push`)
   transfers ownership. If the transfer fails (`Push` returns false), the
   caller still owns it.

Discard paths are where leaks happen, so `queue.Options.OnDrop` receives
every evicted or drained item. For buffers, pass `(*buf.Buf).Release`.

Build with `-tags netxdebug` to panic on double Release and count outstanding
buffers (`buf.Outstanding()`). Tests and load tests always run with it.

## Where shared structures stop

A structure goes into `queue/`, `flow/` or `limit/` if it depends only on
capacity, policy and keys. If it must understand packet contents, it stays in
the gateway: a Media jitter buffer (timestamps, sequence numbers) or a Game
send queue that coalesces position updates are gateway code.

## Freeze rules

1. **Dependency direction is one-way** as drawn above. A change that adds an
   arrow needs a written reason in this file.
2. **New code goes into an existing package first.** A new package needs a
   reason why none fits, recorded here.
3. **Exported names outside `internal/` do not change within a major
   version.**

## Out of scope until measured

- `recvmmsg`/`sendmmsg` batching: next step for UDP; the API does not change.
- io_uring: only if batching is proven to be the bottleneck.
- Reliable UDP: belongs to a protocol layer, not here.
