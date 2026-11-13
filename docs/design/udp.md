# udp — implementation and design specification

## Scope

Whole datagrams in and out, with the peer address and explicit buffer ownership. UDP is not TCP with another name: a datagram is delivered complete or not at all.

The UDP package also exposes bounded batch operations. Original destination metadata, automatic segmentation and coalescing, session routing, and receive loops belong to callers.

## API

| Symbol | Reason |
|---|---|
| `Packet{Buffer *buf.Buf; Addr netip.AddrPort}` | One datagram and its peer: the source when received, the destination when sent |
| `ReadPacket(c *net.UDPConn, max int) (Packet, error)` | One complete datagram of at most `max` bytes, or an error |
| `WritePacket(c *net.UDPConn, p Packet) error` | One datagram, sent whole or reported as failed |
| `ErrTruncated` | A datagram longer than `max`; it is not delivered |

Functions over the standard connection, not a `Conn` type: deadlines, `SyscallConn`, `ReadMsgUDP` and `WriteMsgUDP` stay in the caller's hands. No `Listen`: that is `net.ListenConfig` with `socket.ListenControl`.

## Contract

**[Ownership](../ARCHITECTURE.md#ownership).** A successful read gives the caller `Packet.Buffer`, to release once. A write borrows the packet: the caller still owns and releases it, whether the write succeeded or failed. `Packet` is passed by value; copying the struct copies neither the buffer nor its ownership. No reference count.

**Receive.**

| Arrives | Result |
|---|---|
| A complete datagram | The packet |
| A zero-length datagram | A packet with an empty buffer |
| A datagram over the limit | `ErrTruncated`, and no packet |
| An error | The error, and no packet |

**Truncation.** The read goes into a buffer of `max` plus one byte. A datagram of exactly `max` bytes is complete; one that fills the extra byte is over the limit. No message flag and no per-platform code are involved. Windows reports an oversized datagram as an error instead, and that error is mapped to `ErrTruncated`.

**Right-sizing.** A datagram of 2KiB or less, read into a larger buffer, is copied into a buffer of its own size class and the large one is released at once, so a queued packet does not pin a 64KiB block. The threshold is fixed, not an option, and is checked by a benchmark.

**Receive limit.** The caller passes `max` on every read. 65535 holds every legal UDP payload, so nothing is truncated; a protocol with a known datagram size passes less. `max` is independent of `buf.Large`: a larger buffer is simply not pooled.

**Memory.** A blocked read holds a buffer for `max` bytes and nothing else is held between reads: 64KiB per waiting socket at 65535, 2KiB at 1500. Waiting for readability before allocating would remove that cost without changing this API; it is not in V1.

**Send.** A datagram is atomic.

| The connection reports | Result |
|---|---|
| All bytes, nil | nil |
| An error | That error |
| Fewer bytes, nil | `io.ErrShortWrite` |

The rest of a datagram is never sent separately. After an error the peer may still have received it, so nothing is resent.

**Address.** `Packet.Addr` is canonical and never IPv4-mapped. On receive the peer address goes through `Addr.Unmap()`, so one peer has one value whatever socket it arrived on; the port is unchanged. On send the destination is normalized the same way before use. Native IPv6 addresses, zones included, and NAT64 addresses are left as they are. `Addr` is the remote peer only, never the local or original destination.

| Socket reports | `Packet.Addr` |
|---|---|
| `192.0.2.1:1234` | `192.0.2.1:1234` |
| `[::ffff:192.0.2.1]:1234` | `192.0.2.1:1234` |
| `[2001:db8::1]:1234` | `[2001:db8::1]:1234` |

Normalizing does not make every socket reach both families. A destination the socket cannot send to is an error; it is never rewritten and never sent through another socket.

**Connected and unconnected.** One `Packet` type for both. Unconnected: any source is received and each packet names its destination. Connected: only the peer's traffic is received and every packet goes to the peer; `Addr` is not consulted, so a received packet can be sent straight back.

**[Cancellation](../ARCHITECTURE.md#cancellation).** No per-call context and no goroutine. A blocked read or write is released by the connection's deadline or `Close`, both of which stay available to the caller.

## Batch and packet adapters

| API | Contract |
|---|---|
| `ReadBatch(c, dst, max)` | Wait for one datagram, then read up to the available slots and platform limit. The caller owns all returned buffers, including with a non-nil error. An oversized datagram is discarded and reported; other complete datagrams from that receive remain available. |
| `WriteBatch(c, packets)` | Borrow packets and report the exact accepted prefix. Stop at the first error. |
| `NewPacketConn` | Adapt a UDP socket to packet I/O with a fixed receive limit and successful packet counters. `*PacketConn` satisfies `bufio.PacketReader` and `bufio.PacketWriter`; `udp.Packet` is an alias of `bufio.Packet`. |
| `(*PacketConn).WaitReadPacket()` | On Linux and macOS, wait for readability before allocating. Serialize with other reads on that socket. Windows uses the ordinary receive path. |
| `OffloadSupport(c)` | Probe kernel segmentation and coalescing options. Socket settings and device capabilities are independent. |

Linux amd64/arm64 uses `recvmmsg` and `sendmmsg`. macOS uses extended message receive and can batch connected nonempty sends. Unconnected sends and empty datagrams use the ordinary macOS send path. Other platforms receive one packet per batch call and send in order. System call support errors select the portable path only where no packet can be duplicated. Per-datagram errors preserve accepted progress. Batches are bounded to 32 datagrams per native operation. Each read returns the peer address in canonical form. Socket deadlines and close apply to readiness and transfer.

Automatic segmentation and coalescing are disabled by default. Use these APIs on sockets with receive coalescing disabled so each receive corresponds to one datagram.

## Peer adapters

`Bind` takes ownership of a connected socket whose peer matches the requested canonical address. It exposes packet, batch and `net.Conn` operations. The kernel isolates the peer. A byte-slice read consumes one datagram and returns `ErrTruncated` if the slice cannot hold it.

`NewNATPacketConn` owns a bound socket and exposes its peer under one immutable logical address. Reads, readiness reads and batch reads translate the peer address. Writes require the logical destination and report an accepted prefix on batch failure. The adapter delegates deadlines and close to the owned socket. Route selection, socket sharing and flow lifetime belong to callers.

`NewFallbackPacketConn` exposes an endpoint as `net.PacketConn`, preserving the source address and datagram boundary. Moving a datagram from one endpoint to another is `transfer.CopyPacket`; `udp` has no forwarding function.

## Evidence

macOS, Go 1.25.1, loopback, a 3000-byte datagram:

- `ReadMsgUDPAddrPort` into 1500 bytes returns 1500 with `MSG_TRUNC` set; a 1500-byte datagram returns 1500 without it.
- `ReadFromUDPAddrPort` into 1501 bytes returns 1501: reading into the limit plus one byte detects an oversized datagram with no flag.
- A dual-stack socket reports an IPv4 peer as `[::ffff:127.0.0.1]`, which does not compare equal to `127.0.0.1`.

Ferry reads UDP with `net.ListenUDP` and `ReadFromUDP` into a slice allocated per call, and does not detect truncation. It uses nothing from the old `udp` package.

## Implementation boundary

**Implemented:** `PacketConn` wraps `*net.UDPConn`, stores a receive maximum, atomically counts successful packet operations, and closes its underlying socket once via `sync.Once`. `NewPacketConn(c,max)` panics when `max < 0` or `max == math.MaxInt`; `PacketCounts() (read,written uint64)` reports packet counts, **not bytes**. `WaitReadPacket` must be serialized with other reads on the same socket. `NATPacketConn` is a one-peer address translation adapter: its successful constructor takes a `BoundPacketConn`, validates the logical peer, rewrites received peer addresses, rejects writes to other logical destinations and delegates Close/deadlines. It is **not** a NAT association table.

**Designed, not implemented:** reusable NAT association registry, idle expiry, generation-safe removal and bounded drain. Those require a separate reviewed contract; see [lifecycle](lifecycle.md).

**Deferred:** general packet routing/flow tables, async queue, server lifecycle and application policy.

## Open questions

A future association manager's API and expiry policy are intentionally unresolved; they do not affect the current adapter contract.

## Acceptance

- Each row of the receive and send tables, IPv4 and IPv6, connected and unconnected.
- Addresses: an IPv4 socket, a dual-stack socket and an IPv6-only socket; a mapped and a native address for one peer compare equal; a reply sent to a received address arrives; an IPv4 destination on an IPv6-only socket fails; an IPv6 zone survives.
- A datagram of exactly `max`, one byte over, and zero length.
- Every received buffer released once; a failed write leaves the packet with the caller.
- A read blocked on an idle socket released by a deadline and by `Close`.
- Allocations per received and per sent datagram; benchmark recorded.
- Right-sizing: a small datagram arrives in a small buffer, a large one is not copied; the threshold's benchmark.
- Everything above on Linux and macOS.
