# Changelog

## v1.0.0

Rewrite around the frozen package layout in ARCHITECTURE.md. Not compatible
with v0.x.

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
- `socket.Options.RecvBuffer` and `SendBuffer`.
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
  service; abort waits at most `AbortGrace`.

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
