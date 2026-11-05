# Package buf — design

Overall design: [ARCHITECTURE.md](../../ARCHITECTURE.md).

## 1. Scope

| Owns | Does not own |
|---|---|
| Pooled byte buffers in three size classes | I/O (see `stream`) |
| The ownership rules every go-netx package follows | Encoding |
| Charging in-use memory to a `limit.Budget` | Process memory limits |
| Debug-build ownership checks | |

Imports `limit` only.

## 2. API

```go
const Small, Medium, Large = 2 << 10, 16 << 10, 64 << 10
func NewPool(budget *limit.Budget) *Pool // budget may be nil
func (*Pool) Get(n int) *Buf             // len(B) == n; nil when over budget
func (*Pool) Copy(data []byte) *Buf      // borrowed → owned
type Buf struct{ B []byte /* … */ }
func (*Buf) Release()
const Debug bool                          // true with -tags netxdebug
func Outstanding() int64                  // debug builds
```

## 3. Ownership rules

1. **Borrowed**: data handed to a callback (`udp.Packet.Data`,
   `stream.Reader.Peek`) is valid only until the callback returns or the
   next call. Never release it; `Copy` it to keep it.
2. **Owned**: a `*Buf` from `Get` or `Copy` has exactly one owner, who
   releases it exactly once.
3. **Transfer**: handing a `*Buf` on (`queue.Ring.Push`, `stream.Writer.Add`)
   transfers ownership. If the transfer fails, the caller still owns it.

Discard paths are where leaks happen, which is why `queue` has `OnDrop` and
`stream.Writer` releases on every Flush outcome.

## 4. Design

**Three classes** (MTU datagram, typical stream read, largest datagram):
few classes keep `sync.Pool` hit rates high. Requests above `Large` are
allocated directly and not pooled, but still budgeted.

**`*Buf`, not `[]byte`, in the pool.** Putting a slice into `sync.Pool`
allocates a header on every Put; a pointer does not. `Get` sets `B` to the
requested length over the full class capacity; `Release` sets `B` to nil so
a stale reference fails fast instead of reading reused memory.

**Budget = in-use memory only.** `Get` charges the class capacity before
taking a buffer and `Release` refunds it; a refused `Get` charges nothing.
Released buffers may stay in `sync.Pool` until the GC reclaims them, so the
budget bounds how much data the gateway holds at once, not process RSS.

**Contents are not zeroed** on Release (cost); `Get(n)` exposes only `n`
bytes, and callers overwrite what they use.

**Debug builds** (`-tags netxdebug`): each Buf tracks whether it is owned;
releasing twice panics, and `Outstanding()` counts unreleased buffers.
Release builds compile these checks away. Tests and stress runs use both.

## 5. Concurrency

`Pool` is safe for concurrent use. A `Buf` belongs to one owner at a time.

## 6. Performance (2 vCPU)

Get + Release about 11ns, 0 allocations.

## 7. Decisions

D-05 (budget counts in-use memory only).

## 8. Testing

BUF-01…BUF-14, NF-11 (fuzz) in [docs/TESTING.md](../TESTING.md).

## 9. Open items

None.
