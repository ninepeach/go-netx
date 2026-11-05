# Changelog

## v1.0.0

First stable release. Rewrite around the frozen package layout in
ARCHITECTURE.md; not compatible with v0.x.

Every checkpoint in docs/TESTING.md is covered except UDP-20 (IPv6 send
boundary: test written, not yet run on an IPv6 host). Coverage includes
fuzz tests (`make fuzz`), stress tests with goroutine and file-descriptor
leak checks (`make stress`) and benchmarks. The document is kept in sync
with the tests mechanically (`make verify`).

### Added

- `flow`: process-wide flow IDs that double as epochs, Open → Closing →
  Closed state machine, sharded `Table` with idle eviction and epoch-checked
  `Remove`.
- `limit`: `Limiter` with `Concurrency`, token-bucket `Rate` and `All`;
  `Budget` for total resource caps.
- `queue`: bounded `Ring[T]` with `Reject`/`DropOldest`, `OnDrop`,
  `PushWait`/`PopWait`, `Close`/`Drain` and stats.
- `buf`: `Pool` with three size classes, optional `Budget`, ownership rules,
  and `-tags netxdebug` double-release and leak checks.
- `netx.Group` to run several services under one runtime.
- `socket.Options.RecvBuffer` and `SendBuffer`, applied before bind; TCP
  listeners pass them to accepted connections.
- `udp.MaxDatagram` (65000): the size limit in both directions. `MaxPacket`
  defaults to it and may not exceed it.
- `udp.ErrNilPacket`, `udp.Stats.KernelDropsEnabled`.
- `Stats.KernelDrops` survives the kernel's 32-bit counter wraparound.
- `Stats()` snapshots on `tcp.Server`, `udp.Conn` and `queue.Ring`.

### Changed

- `udp`: reader goroutines call the handler inline with a borrowed buffer
  (`Options.Readers`, one socket per reader with `ReusePort`). No
  per-packet goroutine or allocation; `netip.AddrPort` addresses; oversized
  datagrams dropped and counted; Linux kernel drop counts.
- `tcp`: handler receives `*tcp.Conn` with `ID()` and `Draining()`; admission
  through `Options.Admit`; three-phase shutdown where the handler context is
  cancelled only on abort; `Serve` returns after all handlers.
- `netx.Server`: stop signals start the drain instead of cancelling the
  service; abort waits at most `AbortGrace`; a second signal aborts at once;
  `OnStop` gets its own `ShutdownTimeout` budget.
- Retry backoff in the TCP accept loop and UDP readers stops immediately on
  Close or Shutdown.
- `limit`: `Budget` and `Concurrency` are overflow-safe and never exceed
  their maximum, even transiently; releasing more than was acquired panics.
- `socket`: `UnsupportedIgnore` ignores only "not supported" errors; other
  setsockopt errors are returned.
- `flow`: `Sweep` advances the table clock, so hand-driven sweeps do not
  evict touched flows; touches happen under the shard lock.
- `udp`: fatal read errors close the Conn and are returned by `Serve`;
  `Reply` never panics on a nil or foreign packet.

### Removed

- `mux` package (protocol layer).
- `netx.TCPServer`, `netx.UDPServer`, `ListenAndServeTCP/UDP`,
  `NewTCPService/NewUDPService`: transports implement `netx.Service` directly.
- `udp.Client` and `Exchange`; `tcp.Dial`; `OnError`, `OnOpen`, `OnClose`
  hooks; `MaxConnections` and `MaxHandlers` (use `limit`);
  `Server.OnServe/OnShutdown` (use `ServiceFuncs`).

## v0.2.0

- Add TCP `OnOpen` and `OnClose` connection lifecycle hooks.
- Add optional TCP `DialEvent` callbacks with target, timing, addresses, and
  dial result.
- Add context-aware UDP listener creation with `udp.ListenContext`.
- Add UDP socket configuration through `udp.ListenOptions` and
  `netx.UDPServerOptions.Socket`.
- Preserve the v0.1 TCP, UDP, lifecycle, and high-level server APIs.
