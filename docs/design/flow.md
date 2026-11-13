> **Status: Deferred / non-normative historical proposal.** This document does not authorize a V1 implementation or describe current exported API. For current contracts see [module status](README.md) and [architecture](../ARCHITECTURE.md). 

# flow — draft

## Scope

Transport flow identity, close state and expiration for UDP flows: idempotent close, timeout, cancel reason. No user sessions, and no second copy of TCP connection state.

## Open questions

1. **Consumer.** No module in the tree uses flow. Name the consumer before designing ([P4](../ARCHITECTURE.md#principles)).
2. **Key.** What identifies a flow.
3. **Expiration.** Who drives it: the caller, or a timer here.
4. **[Cancellation](../ARCHITECTURE.md#cancellation):** close reason and callback timing.
5. API and concurrency.
