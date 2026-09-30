# go-netx Core Implementation Constraints

This file defines non-negotiable constraints for coding agents and contributors working on core `go-netx` runtime code.

It is not a product roadmap and not a task list. It exists to prevent unsafe implementations from violating the canonical architecture baseline.

Canonical design documents:

- Chinese: https://github.com/ninepeach/architecture-docs/blob/main/projects/go-netx/design.zh.md
- English: https://github.com/ninepeach/architecture-docs/blob/main/projects/go-netx/design.md

## 1. Fixed Priorities

The implementation priority is fixed:

```text
Correctness > Lifecycle Safety > Memory Boundaries > Simplicity > Observability > High-Concurrency Performance > Feature Count
```

Do not trade lifecycle safety, memory boundaries, or deterministic shutdown for short-term throughput.

## 2. Goroutine Rules

Every goroutine must have:

- a clear owner;
- a bounded lifetime;
- a cancellation path;
- an exit condition;
- a panic boundary when it runs user code;
- a test proving it does not leak.

Do not create goroutines casually in hot paths.

Forbidden by default:

- unbounded per-packet goroutines;
- unbounded per-frame goroutines;
- goroutines without context or shutdown path;
- goroutines that wait forever on a channel, mutex, or network read without cancellation;
- background goroutines hidden behind constructors.

TCP may use one handler goroutine per accepted connection only when connection count is bounded.

UDP must not default to one goroutine per packet. The default UDP model must be a bounded worker pool or an equivalent bounded execution model.

## 3. TCP Server Rules

TCP server implementation must satisfy:

- `Accept` loop exits when context is cancelled or listener is closed.
- Active connections are tracked.
- Connection count is bounded by configuration or a safe default.
- `Shutdown` closes the listener first, then waits for handlers.
- If shutdown times out, active connections are force-closed.
- User TCP handlers are executed behind panic recovery.
- `OnOpen`, `OnClose`, and `OnError` are treated as user callbacks and must not be allowed to crash the process.
- Callback concurrency semantics must be documented.

A TCP connection handler must release its registry entry, close its connection, and decrement all counters exactly once.

## 4. UDP Server Rules

UDP server implementation must satisfy:

- one socket-level read loop by default;
- bounded packet queue or bounded handler execution;
- bounded worker count;
- no default unlimited `MaxHandlers`;
- no default unlimited packet backlog;
- queue-full behavior must be explicit: drop, reject, or backpressure;
- dropped packets must release their buffers and memory leases immediately;
- user UDP handlers are executed behind panic recovery;
- shutdown closes the packet connection and unblocks `ReadFrom`.

The default UDP behavior must protect the process under packet bursts.

## 5. User Callback Rules

The following are user-code boundaries:

- TCP handler;
- UDP handler;
- `OnOpen`;
- `OnClose`;
- `OnError`;
- lifecycle hooks such as start, stop, shutdown, and serve callbacks.

All user-code boundaries must be panic-safe.

Callbacks may be called concurrently unless explicitly documented otherwise. Therefore callback callers must not assume callback state is safe.

If a callback ignores context cancellation, `go-netx` cannot safely kill that goroutine. Implementations must still stop accepting new work, release owned resources, and return timeout errors predictably.

## 6. Context and Blocking Rules

Any operation that may block must accept `context.Context`, a deadline, or be interruptible through owned resource closure.

Forbidden by default:

- unbounded blocking writes;
- unbounded blocking sends to internal channels;
- unbounded waits for queue capacity;
- `Shutdown` implementations that can wait forever without timeout;
- write APIs that may block but cannot be cancelled.

Blocking write paths must be context-aware.

## 7. Backpressure Rules

No internal queue may grow without a configured capacity or memory budget.

Default strategies:

| Scenario | Default behavior |
|---|---|
| TCP / reliable flow | bounded queue + context-aware blocking |
| UDP latest-state | drop old, keep latest |
| UDP reliable command | bounded retry queue + memory budget |
| slow consumer | backpressure first, close on sustained violation |
| memory pressure | reject new work before degrading active work |

A single flow must have a maximum number of concurrent blocked write waiters. Exceeding that limit must return `ErrBackpressure` or a more specific wrapped error.

## 8. Memory Rules

Significant runtime memory must be accounted for through `MemoryAccount` / `MemoryLease`.

Rules:

- reserve before significant allocation when possible;
- successful reserve returns a lease;
- release is idempotent;
- release must happen on success, failure, drop, parse error, send completion, and close;
- panic-safe paths must use `defer lease.Release()` where appropriate;
- hot paths must avoid forcing `MemoryLease` through interface calls when that causes escape or allocation;
- debug builds may track caller stacks for outstanding leases;
- debug tracking must not be on the default hot path.

Do not implement a Go runtime-level custom allocator.

## 9. Buffer Ownership Rules

Buffer reuse is allowed only with explicit ownership.

Rules:

- after `Put`, the previous owner must not read or write the buffer;
- cross-goroutine buffer handoff must be ownership transfer;
- business data kept after a callback returns must be copied;
- never return dirty buffers to user code;
- oversized buffers must not be retained in normal pools;
- pool miss affects only performance, not correctness.

## 10. Protocol Resource Release Rules

A frame, packet, or message rejected during parsing or validation must release all parser-owned resources before returning the error.

This includes:

- invalid session epoch;
- stale frame;
- duplicate frame;
- malformed frame;
- unsupported protocol version;
- failed authentication or handshake validation.

Specifically, returning `ErrInvalidSessionEpoch` must not leave buffer ownership or memory leases to upper layers.

## 11. Status and Metrics Rules

`SimpleStatus` must be cheap and must not perturb the I/O hot path.

Rules:

- do not scan all flows to generate `SimpleStatus`;
- do not take mutexes that contend with I/O hot paths;
- prefer atomic or cached counters;
- do not allocate large temporary objects;
- cumulative counters are monotonic and are not reset at runtime;
- exporters compute deltas outside the core;
- `uint64` wrap-around follows normal Go modulo arithmetic.

Detailed snapshots may be heavier but must still be concurrency-safe and must not expose mutable internal structures.

## 12. Error Rules

Errors must be classifiable with `errors.Is` / `errors.As`.

Backpressure errors should share a common category, with more specific causes when useful:

- buffer full;
- slow consumer;
- memory budget exceeded;
- write waiters exceeded;
- packet queue full.

Normal close, remote close, timeout, cancellation, protocol violation, invalid epoch, duplicate frame, and stale frame must remain distinguishable.

## 13. Test Requirements

Core code changes must add or preserve tests for:

- panic recovery at user-code boundaries;
- goroutine shutdown after `Close` / `Shutdown`;
- shutdown timeout behavior;
- bounded UDP handler execution;
- bounded TCP connections;
- context-aware blocking writes;
- queue-full behavior;
- memory lease release on success, error, panic, and drop;
- buffer ownership safety;
- race-free status snapshot;
- `go test -race ./...` compatibility.

Tests should include repeated runs for lifecycle-sensitive code.

## 14. Final Rules

Do not merge core runtime code that violates any of these rules:

1. No goroutine without owner and shutdown path.
2. No unbounded queue.
3. No unbounded handler execution.
4. No user callback without panic boundary.
5. No blocking operation without cancellation or resource-close wakeup.
6. No resource without ownership.
7. No memory lease without deterministic release.
8. No buffer reuse without ownership transfer.
9. No UDP hidden reliability in raw transport.
10. No status collection that contends with the hot path.
11. No default unlimited connection or packet concurrency.
12. No implementation shortcut that hides lifecycle failure.
