# buf — design specification

## Scope

`buf` provides fixed-capacity byte buffers, pooled storage, cursor operations, protocol headroom and direct byte I/O. It owns memory mechanics, not framing, socket behavior, retry policy, quotas or network sessions. The buffer has exactly one owner; there is no reference counting.

## Storage and allocation

- Twelve `sync.Pool` classes: 64B, 128B, 256B, 512B, 1KiB, 2KiB, 4KiB, 8KiB, 16KiB, 32KiB, 64KiB and 72KiB.
- `Large = 64 << 10` is the largest power-of-two class; requests above 72KiB are allocated directly and never pooled.
- `Capacity(n)` returns the backing class size, or `n` for unpooled sizes. Negative requests panic.
- `Get(size)` and `GetHeadroom(head, size)` provide an owned, empty buffer. The logical `Cap()` is exactly `head + size`, even when the pooled block is larger.
- Negative sizes and integer overflow panic. Allocation has no built-in memory budget; admission control belongs to the caller.
- No `Reserve` or `OverCap` API. Protocol layers determine required headroom, payload and tail capacity before acquiring a buffer. If requirements change, allocate a new buffer, copy unread data and release the old owner.
- Memory is not zeroed. The caller initializes every byte before it is exposed to I/O.

## Layout and invariants

```text
backing storage: [ consumed/headroom | readable data | writable tail | unused pool slack ]
logical range:   0                 start         end          Cap()
```

`0 <= start <= end <= Cap() <= len(backing)`; `Len() = end-start`. `Reset()` restores the original configured headroom. `Advance(n)` consumes readable bytes and makes them available as front space; `ExtendHeader(n)` prepends into available front space; `Extend(n)` appends into the logical writable tail. `Truncate(n)` keeps the first `n` readable bytes. All cursor bounds are checked.

Protocol framing uses explicit front and tail capacity. Transport or protocol overhead is expressed as required bytes before allocation, not as a hidden change to `Cap()`. Packet MTU checks are the responsibility of the packet layer.

## Public API

| API | Contract |
|---|---|
| `Get`, `GetHeadroom`, `Copy` | Return an owned buffer |
| `As`, `With` | Wrap caller storage as readable data or writable space; do not pool that storage |
| `Capacity`, `Large` | Capacity-class information |
| `Len`, `Cap`, `Bytes`, `FreeBytes` | Report logical size and borrowed views |
| `Extend`, `ExtendHeader`, `Advance`, `Truncate`, `Reset` | Checked cursor operations |
| `Read`, `ReadByte`, `ReadBytes` | Consume readable data; exact reads preserve cursor on insufficient data |
| `Write`, `WriteByte` | Append within fixed logical capacity |
| `ReadOnceFrom`, `ReadFullFrom`, `WriteTo` | Direct `io.Reader`/`io.Writer` integration |
| `Release` | End ownership exactly once |

## Ownership and lifetime

An owned `*Buf` is released exactly once. `Buf` must not be copied or used concurrently. A borrowed slice aliases the backing storage and is valid only until the next operation that can mutate its content or cursors, transfer ownership, or release the buffer. `ReadBytes` returns a borrowed slice of consumed data; its caller must not retain it beyond the documented lifetime. `With` and `As` borrow exclusive access to external storage until release; release drops the alias and never returns that storage to a pool.

Ownership transfers at explicit API boundaries. On a failed transfer, the caller retains ownership unless the API explicitly states otherwise. Buffer-based stream/packet adapters must document whether they consume, return or release a buffer, including error paths. No implicit sharing or reference counting is introduced to support batching.

`Release` clears logical state; a second release and release of a copied handle panic. Use after release is a contract violation, not a detected error: cursor-moving calls panic, but `Len`, `Cap`, `Bytes` and `FreeBytes` are not checked on the hot path and report an empty buffer. The owner check is not a substitute for synchronization: a stale pointer reused after pool checkout remains invalid. A buffer may move between goroutines by ownership transfer, but concurrent access to the same buffer is forbidden.

## I/O and errors

- Invalid cursor sizes panic. `Write` accepts the prefix that fits and returns `io.ErrShortBuffer` for the remainder.
- Nonempty reads from an empty buffer return `io.EOF`. `ReadBytes(n)` is atomic with respect to the cursor and returns `io.ErrUnexpectedEOF` for insufficient nonempty content.
- `ReadOnceFrom` makes one read; `ReadFullFrom` retains valid partial data and returns `io.ErrNoProgress` after 100 consecutive empty reads.
- `WriteTo` consumes only the acknowledged prefix and converts a short nil-error write to `io.ErrShortWrite`.
- Invalid external byte counts never advance cursors. Data and errors returned together are handled without discarding valid data.

## Integration contract

`bufio` owns transmission and receive strategy. It may borrow a `Buf` view during a synchronous call or take ownership explicitly; it must not assume a pooled buffer is shareable. Batch I/O retains owned buffers until completion or discard. Zero-copy stream paths operate on socket/file descriptors without manufacturing or bypassing `Buf` ownership. Protocol adapters reserve header/tail space through `GetHeadroom` and explicit size calculations.

## Concurrency and verification

Pool operations are concurrent-safe; a particular `Buf` is not. Test all twelve size classes, >72KiB allocation, logical capacity versus backing class, overflow, headroom and tail boundaries, reset/reuse, copied handles, double release, borrowed storage, invalid I/O counts, partial reads/writes and cross-goroutine ownership handoff. Fuzz cursor operations against a byte-slice model. Benchmark allocation counts and throughput for small packets, 1500B, 16KiB, 64KiB, 72KiB and larger buffers, including GC pressure.
