> **Status: Deferred / non-normative historical proposal.** This document does not authorize a V1 implementation or describe current exported API. For current contracts see [module status](README.md) and [architecture](../ARCHITECTURE.md). In particular, \`queue.Ring\` is excluded from V1.

# queue — draft

## Scope

A bounded generic ring with explicit capacity, backpressure, and a reject or drop policy. Never unbounded. No protocol semantics.

[Ownership](../ARCHITECTURE.md#ownership): a successful push transfers to the queue; a rejected push leaves the item with the caller.

## Open questions

1. **Consumer.** No module in the tree uses a queue. Name the consumer before designing ([P4](../ARCHITECTURE.md#principles)).
2. **Overflow policy:** reject, drop oldest, or both; and who releases a dropped item.
3. API, concurrency model (single or multiple producers and consumers), close behavior.
