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
reported in `Stats.KernelDrops` on Linux (the kernel reports drops with the
next queued datagram, so the count lags once traffic stops).
`udp.MaxDatagram` (65000) is the size limit in both directions; `MaxPacket`
defaults to it and may not exceed it. Datagrams larger than `MaxPacket` are
dropped and counted, never delivered truncated.

**TCP.** One goroutine per connection on the Go netpoller. `Options.Admit`
decides admission at accept time; rejections are counted, not logged. The
handler context is cancelled only on abort. Temporary accept errors back off
5ms doubling to 1s, and Close or Shutdown cuts the backoff short. The server
holds no per-connection buffers: reading is the handler's job.

## TCP compared with net/http and fasthttp

The TCP transport was reviewed against the two most used Go HTTP servers
(net/http as of Go 1.24.7, fasthttp `000816e`, 2026-11). Gin has no transport
of its own: `gin.Run` builds an `http.Server`, so it shares net/http's.

| | net/http | fasthttp | go-netx |
|---|---|---|---|
| Connection model | goroutine per conn | worker pool reusing goroutines (LIFO) | goroutine per conn |
| Temporary accept errors | 5ms→1s backoff, `time.Sleep` | `Timeout` only, fixed 1s sleep | 5ms→1s backoff, interruptible |
| Permanent error detection | `Temporary()` | string match on the error text | `errors.As` plus a stopped flag |
| Concurrency cap | none | `Concurrency`, replies 503 | `Admit`, closes and counts |
| Per-IP cap | none | `MaxConnsPerIP` | planned (below) |
| TCP keepalive | default 15s, fixed | on/off and period | default 15s, fixed; planned (below) |
| Graceful shutdown | close listener, poll idle conns | close listener, poll every 100ms | `Draining()` channel, no polling |
| After drain timeout | conns keep running | conns keep running | conns aborted automatically |
| Idle connection memory | buffers held for life | `ReduceMemoryUsage` frees them | none held by the server |
| Error reporting | logs | logs, rate-limited | counters (`Stats`) |

### Adopted

- **net/http's backoff curve**, but interruptible: a shutdown never waits
  out a sleep (REG-02).
- **A drain signal instead of hooks or polling**: long-lived handlers select
  on `Conn.Draining()` and tell clients to move; Serve returns through a
  channel when the last handler ends.
- **Abort after the drain timeout.** Both references return `ctx.Err()` and
  leave connections running for the caller to clean up; go-netx closes them
  and cancels handler contexts, so a timed-out shutdown leaves nothing
  behind (verified by the leak checks, RUN-16).
- **Counters, not logs**, on the accept path: a flood of rejected or failed
  accepts cannot flood the log.

### Rejected

Recorded as design decisions D-07 to D-09 in docs/TESTING.md.

- **Worker pool (fasthttp).** It saves goroutine creation and stack growth,
  well under a microsecond per connection. A full accept measures about 79µs
  (`BenchmarkAccept`), dominated by syscalls, so the gain does not pay for
  an idle-worker lifecycle. Revisit only with a benchmark that shows it.
- **Protocol replies on rejection** (fasthttp's 503). The transport does not
  speak the protocol; a gateway that wants to answer before closing does so
  in its own admission logic.
- **Per-connection contexts** (net/http's `ConnContext`). One allocation per
  connection for something gateways rarely need: shared values come from the
  Serve context, and identity from `Conn.ID()`.
- **Matching error strings, fixed sleeps, polling shutdown**: already done
  better here, as the table shows.

### Planned for v1.1.0

Each item gets checkpoints in docs/TESTING.md before it is implemented.

1. **Configurable TCP keepalive.** IM gateways must notice peers that vanish
   without a FIN (a phone switching networks). Add a keepalive setting to
   `socket.Options` based on `net.KeepAliveConfig` (idle time, probe
   interval, probe count), applied to listeners and dialers, with a way to
   disable it.
2. **Per-source admission.** Stop one source from taking every connection.
   `Admit` cannot see the peer, so admission needs the remote address, for
   example a limiter keyed by address in `limit` and a matching option in
   `tcp`. Release must be tied to the connection's close, as fasthttp does
   by wrapping the conn. IPv6 sources must be grouped by prefix (a /64 by
   default): one client can hold an entire /64 and rotate addresses in it.
3. **Idle connections hold no buffers** (a gateway pattern, documented with
   an example rather than built in). 100k idle connections with a 4KB read
   buffer each pin 400MB. Wait for data with a small buffer, then borrow a
   large one from `buf.Pool` for the burst and release it when the
   connection goes quiet, as fasthttp's `ReduceMemoryUsage` does.

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
