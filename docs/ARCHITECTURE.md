# go-netx — architecture

## Goal

Reusable Go networking primitives for IM, game and media gateways and Ferry. The library provides owned memory buffers, buffered/extended network I/O, efficient stream and packet transfer, socket configuration, endpoint address values, transport primitives and existing native counters. Application framing, authentication, routing decisions and user sessions remain with callers.

## Principles

| | Principle | Meaning |
|---|---|---|
| P1 | Strict ownership | Every pooled buffer has one owner; transfers and borrowed views are explicit. No reference counting. |
| P2 | Explicit semantics | No hidden retry, unbounded retention or silent change in packet/stream behavior. |
| P3 | Module boundaries | Memory, network I/O, socket setup, transports and policy have separate responsibilities. |
| P4 | Complete transport capability | Stream and packet I/O, copy sessions, batching, zero-copy and connection adapters are defined as coherent capabilities, with safe fallbacks. |
| P5 | Measured performance | Optimized paths require correctness tests and benchmarks against fallback paths. |
| P6 | Accurate counters | Existing counters must retain documented accounting boundaries without double counting. |

## Layering

This section is the authority for every document and code review. Where another section or a module document disagrees, this one wins.

**Capabilities come from the modules, flow is composed by `transfer`, policy is decided by the application.**

```
Application
    │
    ▼
 transfer ─────────┐
    │              │
    ▼              │
  bufio            │
    │              │
    ▼              ▼
   buf ◄───────────┘
```

An arrow means the upper or left package imports the lower one. `bufio` provides capabilities; `transfer` uses and combines them, so `transfer` sits above `bufio`. Every architecture diagram is drawn in this direction, `transfer → bufio → buf`, so that layering and imports are never confused.

| Package | Responsible for | Explicitly not | Dependency rule |
|---|---|---|---|
| `buf` | Buffers and memory ownership | I/O flow | No other go-netx package |
| `bufio` | I/O capabilities, interfaces, buffer adapters, optimizations | Copy flow, forwarding policy, session management | May depend on `buf`; never on `transfer` |
| `transfer` | Composing readers and writers, two-way forwarding, packet copy, half-close | Socket creation, concrete TCP or UDP, routing | May depend on `bufio` and `buf` |
| `tcp` | Generic TCP listener, admission, tracking and shutdown | Protocol sessions, relay, process policy | May depend on `socket` |
| `udp` | Datagram I/O, batch I/O, socket adapters, peer address translation | NAT association tables, session managers, routing engines | May use `buf` and `bufio`; never `transfer` |

### Lifecycle and services

go-netx provides network primitives with explicit ownership, cancellation,
close, error and release contracts. Cross-service lifecycle orchestration
belongs to applications.

| Layer | Manages | Belongs to |
|---|---|---|
| Resource | Buffer, socket, packet; `Close` and `Release` | The package that defines it |
| Operation | Copy, cancel, half-close, joining its own goroutines, error propagation | `transfer` |
| Service | Generic TCP accept, tracking and drain/abort | `tcp`; cross-service grouping belongs to applications |

An operation such as `transfer.CopyConn` cancels, stops both directions and waits for its own work to end. It does not stop a server, handle signals, restart, or know about other connections.

The `tcp` package owns only generic listener acceptance, bounded admission,
connection identity/tracking and Stop Accept → Drain → Abort. Applications
compose and shut down independent services. Protocol framing,
authentication, routing, process signals, health checks, logger selection and
shutdown timeout policy remain in applications.

Also the application's: connection pools, NAT associations, flows and sessions, retry and reconnect. Statistics are counted natively by each package; there is no central manager. Cancellation uses each I/O module with `context`; there is no general canceler.

A plain TCP proxy composes `net` and `transfer`. A UDP gateway composes `udp`, `bufio` and `buf`. A service that needs custom socket behaviour adds `socket`. A capability that is not needed brings in no abstraction and no runtime.

## Modules

| Module | Responsibility |
|---|---|
| `buf` | Fixed-capacity pooled byte storage, cursors, headroom, borrowed views and direct byte I/O |
| `bufio` | Buffered/extended stream and packet I/O contracts, wrappers, cached reads, read-wait, vectorised and packet-batch capabilities, deadlines and platform adapters; no transfer orchestration |
| `transfer` | Stream and packet copy orchestration, CopySession, bidirectional relay, counters, direct-copy and Linux splice strategy, safe fallback |
| `metadata` | Lightweight endpoint address values: IP or unresolved domain plus port; parsing and explicit conversion, no DNS or routing |
| `socket` | Socket options and platform capability configuration |
| `tcp` | TCP accept loop, bounded admission, connection identity and drain/abort lifecycle |
| `udp` | Datagram transport and socket-level packet behavior; connected/unconnected Bind and single-peer NAT address adapters. Association tables are not implemented |


## Dependencies

`metadata → Go standard library` only; `bufio → buf`; `transfer → bufio/buf`; `udp → bufio/buf/metadata` only where required; `tcp → socket`; `socket` provides low-level platform socket configuration, not transport orchestration. **Neither `bufio` nor `udp` imports `transfer`; `transfer` does not import concrete `tcp`/`udp` implementations.** Higher-level callers compose transports with transfer. `buf` has no internal module dependency. TCP and UDP need not have symmetric package structures. No dependency cycles. Platform-specific implementations live behind build tags or internal helpers; shared capability interfaces stay platform-neutral. External dependencies require explicit review.

## Cross-module contracts

### Ownership

An owned `*buf.Buf` has one owner and is released exactly once; unlike connection Close, a second Buffer Release panics. Borrowed slices are not retained beyond their stated lifetime. Transfer APIs specify success/failure ownership. Shared immutable slices remain owned by the lender and are dropped after the documented operation. Buffer pools do not use reference counting.

### Capability

Extended stream, packet, batch, read-wait and direct-copy operations are exposed through narrow interfaces. A wrapper must explicitly preserve and expose an optimization; raw descriptor access must not bypass encryption, compression, counters, limits or cached bytes. Capability selection may change after a handshake or when cached bytes are exhausted.

### Cancellation

Half-close differs from full close. Read/write deadlines are independent. APIs accepting cancellation must specify how blocked I/O and worker goroutines are released. Socket wrappers that document idempotent Close implement it; buffer Release is deliberately not idempotent. See [TCP](design/tcp.md) and [transfer](design/transfer.md) for the implemented lifecycle and cancellation contracts.

### Fallback

A fast path may be disabled or unsupported without changing byte order, packet boundaries, address metadata, ownership or counters. Unsupported optimizations fall back; semantic errors propagate. After partial success, only confirmed unsent data may be sent again. Routing/interface-binding requirements that affect traffic destination fail rather than silently falling back.

### Existing counters

Counters already implemented by individual packages remain part of those packages. They must accurately describe their accounting boundaries and must not be summed across layers as total traffic. No standalone statistics module, exporter or unified instrumentation framework is planned.

## Decisions

| | Decision |
|---|---|
| D1 | `buf.Get` has no built-in memory budget; resource admission belongs to callers. No standalone admission-limit package is part of go-netx. |
| D2 | Failed buffered stream writes release queued owned buffers and do not implicitly retain unsent data for retry. |
| D3 | `bufio` owns I/O capability contracts and adapters; top-level `transfer` exclusively owns copy orchestration for stream and packet. `udp` owns datagram transport, Bind and peer address translation adapters; it owns no copy engine and no association table. |
| D4 | Module design specifies intended contracts; implementation status is tracked separately in `design/README.md`. |
| D5 | No `buf.Reserve` or `OverCap`; protocol layers allocate explicit headroom and capacity. |
| D6 | No core `queue` or `flow` package. Stream transfer uses synchronous copying and natural backpressure; application-level queues and session registries remain with callers. No corresponding module or design draft is maintained. |
| D7 | `tcp` provides reusable server lifecycle but no dialer or relay. `socket` configures sockets and `transfer` remains the only copy orchestrator. |
| D8 | Add a lightweight `metadata.Endpoint` for IP/domain + port. Keep unresolved domains distinct from concrete socket peers; no DNS resolution, generic application metadata or new `network` package. Preserve `udp.Packet.Addr netip.AddrPort` in V1. |
| D9 | Preserve existing native counters and their documented semantics; application observability remains application-owned. No unified statistics module is planned. |

## Module specifications

- [buf](design/buf.md)
- [bufio](design/bufio.md)
- [transfer](design/transfer.md)
- [metadata](design/metadata.md)
- [Module status](design/README.md)

## Scope exclusions and deferred designs

`queue`, `flow`, `limit`, `pipe`, `observable`, `memory`, `statistics`, and a standalone `lifecycle` package are **not core packages**. Do not add a queue between ordinary stream readers and writers: blocking writes provide backpressure. A UDP batch is a bounded operation, not a requirement for a general asynchronous queue. Application framing, session tracking, routing and admission policy belong to consumers unless a demonstrated reusable primitive warrants reconsideration.

## Target package boundaries and migration

See [module status](design/README.md) for implemented and planned capabilities. Keep existing `bufio.Reader`/`Writer` behavior and API stable while adding capability interfaces. `bufio` must not call `transfer.Copy` (including from adapter `WriteTo`/`ReadFrom` methods); use standard `io.Copy` or a local checked fallback to prevent import cycles. `transfer` may optimize only when wrappers explicitly permit it. `udp` remains compatible with its V1 packet contract; Bind, single-peer address translation and batch operations are optional transport extensions.

## Specification authority and implementation status

The current source and tests are the evidence for **Implemented** behavior; design documents must accurately describe that behavior, not override it. **Designed** features are proposed future contracts and must never be presented as existing exports. **Deferred** features are excluded from V1. For every exported API, document its exact signature, zero/default values, errors, resource ownership, concurrent-use rules, platform behavior and acceptance tests. See [module status](design/README.md) and [TCP](design/tcp.md). The library provides generic TCP drain/abort; service grouping remains application-owned. Process signals, protocol sessions, and shutdown timeout policy belong to applications.
