> **Status: Deferred / non-normative historical proposal.** This document does not authorize a V1 implementation or describe current exported API. For current contracts see [module status](../README.md) and [architecture](../../ARCHITECTURE.md). The \`limit\` module is excluded from V1.

# limit — draft

## Scope

Nonblocking admission and byte-budget primitives. It counts; it does not allocate or pool, and it does not claim to bound process RSS.

Known consumers, both in the application: admission in its accept loop (the old `tcp.Server` took a limiter and a per-IP cap; [tcp](../tcp.md) no longer has a server), and bounding buffer memory around `buf.Get` ([D1](../../ARCHITECTURE.md#decisions)).

## Open questions

1. **API.** The smallest set that serves the two consumers above: a limiter interface, a count limiter, a byte budget, a per-key limiter — which of these, and with what reason each.
2. **Release contract.** How a reservation is given back, including when the admitted work fails, and what a double release does.
3. Concurrency and overflow behavior.
