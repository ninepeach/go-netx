# Statistics — network and buffer accounting design

## Status and scope

Approved target design; **not a claim of implementation**. Statistics are a native go-netx capability, owned by the relevant modules, not a generic `observe` or event-bus package. Applications own metrics collection schedules, dashboards, Prometheus/OpenTelemetry exporters, process-wide runtime metrics, and user/session attribution.

## Accounting boundaries

| Module | Suggested counters / gauges | Accounting boundary |
|---|---|---|
| `buf` | allocations, reuses, outstanding buffers/capacity; optional retained-pool capacity | Pool checkout/return, not process RSS or Go heap |
| `bufio` | read/write bytes, calls, errors (optional instrumented wrapper) | Bytes actually returned by Read or accepted by Write |
| `transfer` | completed copies, bytes transferred, splice attempts/success, fallbacks, errors | Successfully forwarded bytes at the transfer boundary |
| `udp` | received/sent packets and bytes, errors, batch operations | Successfully received datagrams and successfully transmitted datagrams |
| `socket` | configuration failures (optional) | Control hook failures, not application connection failures |

Counts at different layers **must not be summed** as total traffic: the same bytes can traverse `udp`, `bufio`, and `transfer`. Consumers choose one accounting boundary for each metric.

## Required semantics

- Counters are cumulative and monotonic for the lifetime of their scope; gauges represent a point-in-time quantity. Resetting a scope must be explicit, not silently inferred.
- A successful read increments bytes by the returned `n`, including `n > 0` with a non-nil error. A write increments bytes by the number of bytes actually accepted; short writes and errors are accounted separately.
- A successful UDP receive/send increments packet count once per datagram; batch paths count each completed datagram, not each syscall. Define payload bytes versus wire bytes explicitly: V1 counts **payload bytes**, excluding IP/UDP/TCP headers.
- `transfer` bytes mean confirmed forwarded bytes, not merely bytes read from a source. Splice/fallback paths must obey the same boundary; a partial fast path followed by fallback must never double-count.
- `buf` outstanding capacity is the sum of **capacities** of checked-out pooled buffers, not their used lengths. Distinguish outstanding bytes from retained idle pool capacity and from OS/process memory. A pool cache hit is not the same as avoiding all runtime allocation.
- Counters do not imply packet loss detection, network-interface traffic, CPU usage, process RSS, heap usage, or per-user traffic unless an application explicitly provides those measurements.
- Stats snapshots are safe under concurrency. A snapshot may be eventually consistent across fields; document that it is not an atomic cross-module transaction. Overflow behavior and lifetime must be documented before public API finalization.

## API and performance approach

- Keep stats types and optional `Stats()`/`Snapshot()` accessors within their owning packages; names are illustrative, not yet public API commitments.
- Reuse existing counters and packet accounting where possible. Avoid duplicate counters on the same I/O path and preserve existing exported APIs.
- Prefer per-instance statistics for connection/transfer accounting and explicitly scoped pool-wide statistics for pooled memory. Do not require a global singleton.
- Disabled instrumentation must avoid per-operation atomics, heap allocations, channel sends and background goroutines. When enabled, prefer bounded atomic counters or low-contention instance-local accounting; measure overhead.
- No per-read/per-packet event publishing, observer/subscriber hub, hidden timers, network exporter, third-party telemetry dependency or mandatory application callbacks.
- Do not retain `*buf.Buf`, payload bytes or connection references in statistics snapshots.

## Ownership and lifecycle

- Buffer checkout increments outstanding counts once; return decrements once. Do not interpret a pooled buffer's reuse as a new allocation. Preserve single-owner release rules.
- Scope lifetime, close behavior and post-close snapshot validity must be explicit; closing a connection must not erase historical cumulative counters unexpectedly.
- Statistics never alter packet boundaries, error propagation, cancellation, deadlines or transfer fallback behavior.

## Verification gate

1. Table-driven tests for exact read/write and datagram counts, partial writes, `n > 0, err != nil`, short writes and errors.
2. Transfer tests for splice success, unsupported fast paths, partial splice then fallback, and cancellation, checking no duplicate counting.
3. Buffer tests for allocation versus reuse, outstanding capacity, return-to-pool and concurrent checkout/return.
4. Race tests for snapshots during active traffic and concurrent close.
5. Benchmarks for instrumentation disabled/enabled across representative TCP copy, UDP batch and buffer checkout paths; report allocations/op and throughput/latency differences without inventing a zero-overhead claim.
6. Run supported Linux/macOS test gates; Windows compile gate where applicable. Mark unexecuted platform tests as unverified.

## Rollout

First inventory existing counters and document their real semantics. Implement opt-in per-instance traffic stats and pool stats incrementally, with no public API breaks. Review semantics and benchmark results before introducing shared interfaces. Keep generic server observability at the application layer.
