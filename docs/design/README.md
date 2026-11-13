# Module designs

## Status

Design documents describe the target contract. A documented capability is not a claim that the current source tree already implements it.

| Module | Design | Code in tree |
|---|---|---|
| [buf](buf.md) | target specification | Core pool, cursor and byte I/O implemented; Linux test gate to verify |
| [bufio](bufio.md) | target specification | Adaptive Reader, gathered Writer, extended capabilities, cache, TCP read-wait, counter unwrapping, vectorised write, packet batch interfaces and directional adapters implemented; multi-buffer vectorised read-wait not implemented |
| [metadata](metadata.md) | Endpoint V1 contract | Implemented; macOS and Linux Metadata tests passed, Windows amd64 cross-compiled |
| [transfer](transfer.md) | new target specification | Synchronous stream Copy with counter-aware path selection, Linux splice for eligible counted stream sockets (compiled, **not yet run on Linux**), CopySession, bidirectional half-close, CopyPacket and batch CopyPackets implemented; multi-buffer vectorised read-wait remains unimplemented |
| [tcp](tcp.md) | reusable TCP server contract | Implemented; macOS unit/race/vet gate passed |
| [udp](udp.md) | Datagram and peer adapter contracts | Packet read/write, native batching, Bind and peer address translation implemented |
| [socket](socket.md) | control hook contract | Platform control hooks implemented |

## Core package boundaries

- `buf`: pooled memory, cursors and explicit single-owner release; owns pool statistics.
- Existing native counters remain owned by their modules; no separate statistics module or unified statistics contract.
- `bufio`: I/O capabilities, buffered readers/writers and wrappers; **no transfer orchestration**.
- `transfer`: protocol-neutral stream/packet copy, counters, half-close and optimized dispatch; **no dependency on concrete `tcp` or `udp`**.
- `metadata`: address-only IP/domain + port values and explicit conversions; no DNS, routing or sessions; standard library dependencies only.
- `socket`: platform-specific socket configuration; **no connection/session manager**.
- `udp`: datagram-specific I/O, batching, Bind and peer address translation adapters; **no copy engine, association table or session manager**.
- `tcp`: protocol-neutral accept/admission/drain/abort; no dialer, framing or relay.

Only the seven implemented packages are in scope. Application queues, flow/session registries, admission policy and cross-service lifecycle remain outside go-netx. No additional modules are planned.

## Design and implementation gate

**Implemented** statements must match actual exported source and observable behavior. **Designed** statements describe future contracts, and **Deferred** statements do not authorize V1 implementation. A target design defines scope, exact signatures (for existing exports), interfaces, ownership, error semantics, platform behavior and acceptance tests. Implementation is tracked independently: not started, in progress, implemented, and verified. A capability is verified only after tests pass on the relevant platforms. Interface names for newly designed capabilities are finalized during implementation review without weakening the documented behavior.

## Document structure

Scope · Architecture · API and behavior · Ownership/capability/cancellation/fallback contracts · Platform behavior · Acceptance and benchmarks.

## Implementation order

1. Implement and test the independent `metadata.Endpoint` V1 contract without changing existing UDP packet addresses; preserve and verify existing `buf` and buffered `bufio` contracts.
2. Extended stream/packet interfaces, cached connection wrappers and capability propagation.
3. `bufio` read-wait and vectorised/batch capability adapters; validate one-way imports.
4. `transfer` Copy, CopySession, counters, bidirectional relay and safe fallback.
5. Linux splice and cross-platform transfer fallback.
6. Validate existing UDP packet batching, Bind, peer address translation and connection ownership; extend only where tests or use cases justify it.
7. Verify existing counters against their current code and API contracts; do not introduce a new statistics module.
8. Cross-platform regression, race, fuzz and performance gates.
