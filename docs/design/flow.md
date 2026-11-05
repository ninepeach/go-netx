# Package flow — design

Overall design: [ARCHITECTURE.md](../../ARCHITECTURE.md).

## 1. Scope

| Owns | Does not own |
|---|---|
| Transport identity: a flow ID that doubles as an epoch | I/O (transports move bytes) |
| The close state machine (Open → Closing → Closed) | Sessions: users, login, reconnects |
| A sharded table keyed by anything comparable, with idle expiry | Authentication |

A flow is one TCP connection or one UDP source address; it ends when the
transport ends. A session is a logged-in user that survives reconnects and
belongs to the gateway. `flow` imports nothing from go-netx, and transports
do not import `flow`: a gateway looks up `udp.Packet.From` or
`tcp.Conn.ID()` in a `Table`.

## 2. API

```go
func NextID() uint64                    // process-wide, strictly increasing, never 0
type Flow[V any] struct{ Value V /* … */ }
func New[V any](value V) *Flow[V]
func (*Flow[V]) ID() uint64
func (*Flow[V]) State() State           // Open, Closing, Closed
func (*Flow[V]) BeginClose() bool       // Open → Closing, true once
func (*Flow[V]) Close() bool            // → Closed, true exactly once
func (*Flow[V]) LastSeen() int64

func NewTable[K comparable, V any](TableOptions[K, V]) *Table[K, V]
type TableOptions[K comparable, V any] struct {
    Shards      int           // power of two, default 64
    IdleTimeout time.Duration // 0 = no expiry
    OnClose     func(K, *Flow[V]) // exactly once per flow leaving the table
}
func (*Table) Get(K) (*Flow[V], bool)
func (*Table) GetOrOpen(K, newValue func() V) (*Flow[V], bool)
func (*Table) Remove(K, id uint64) bool // only if the ID still matches
func (*Table) Sweep(now time.Time) int
func (*Table) Run(context.Context) error // clock + sweeps; closes all on exit
func (*Table) Close()
func (*Table) Len() int
func (*Table) Range(func(K, *Flow[V]) bool)
```

## 3. Design

**ID as epoch.** IDs are global and strictly increasing, so a stale callback
that remembers `(key, id)` can never remove a newer flow that reused the
key: `Remove(key, id)` checks the ID. No separate epoch field is needed.

**Exactly-once close.** `Close` uses CAS; exactly one caller gets `true`, so
releasing resources on `true` is safe. The table calls `OnClose` after the
flow is Closed and outside shard locks, for every path out of the table:
`Remove`, idle expiry, `Close`.

**Sharding.** Keys hash with `maphash.Comparable` to one of a power-of-two
number of shards, each a mutex and a map, padded to its own cache line.
A lookup locks one shard.

**Coarse clock.** Touches record the table's clock, not `time.Now()`, which
would cost a call per packet. `Run` advances the clock every quarter of
`IdleTimeout` (clamped to 10ms…1s) and sweeps. `Sweep(now)` also advances
the clock, so a table swept by hand without `Run` does not evict touched
flows (REG-11); the clock never moves backwards.

**Touch under the lock.** `Get` and `GetOrOpen` update `LastSeen` while
holding the shard lock, so `Sweep` cannot evict a flow between lookup and
touch (REG-15). A returned flow can still be closed later; callers that
care check `State`.

## 4. Performance (2 vCPU)

- `Get` on `netip.AddrPort`: about 64–73ns at 1k flows, 243ns at 100k,
  385ns at 1M (cache misses), 0 allocations.
- `Sweep` with nothing to evict: about 16ns per flow at 10k, 34ns at 100k,
  105ns at 1M; a million flows cost about 100ms per sweep, roughly 10% of a
  core at the 1s maximum tick.

## 5. Decisions

D-06 (pending): sweep stays a full scan until a gateway approaches a
million flows, then becomes incremental (a few shards per tick).

## 6. Testing

FLOW-01…FLOW-16, NF-05 (stress), NF-09 (fuzz), NF-14 in
[docs/TESTING.md](../TESTING.md).

## 7. Open items

- Incremental sweep (D-06).
