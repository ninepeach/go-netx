# Changelog

## v1.1.0 (unreleased)

### Added

- `socket.Options.KeepAlive`: TCP keep-alive idle time, probe interval and
  probe count, or `Disable`. Applies to listeners (inherited by accepted
  connections) and dialers; the zero value keeps Go's default.
- `tcp.Options.MaxConnsPerIP` and `IPv6PrefixBits`: per-client-address
  connection cap, local to one server, with IPv6 grouped by prefix
  (default /64). `tcp.Stats.RejectedPerIP` counts refusals.
- `limit.PerKey[K]`: per-key concurrency limiter that forgets keys at zero.
- `stream` package: `Reader` keeps a 512B buffer while idle and borrows
  larger ones from a `buf.Pool` only while data is pending (`Peek`,
  `Discard`, `Fill`, `Read`, `Max`, `ErrTooLarge`, `ErrNoBuffer`);
  `Writer` sends queued `*buf.Buf`s with one writev and releases them.
- `tcp.Conn.WriteBuffers`, so writev survives the `*tcp.Conn` wrapper
  (`stream.BuffersWriter`).
- `make verify` also runs the tests without `-race`, where allocation
  assertions are not skipped; `make fuzz` caps input minimization
  (`FUZZMIN`).

- udp: `Options.Batch` receives up to that many queued datagrams per system
  call with Linux `recvmmsg` (default 8 on Linux, 1 elsewhere, max 1024).
  Under load the receive cost per datagram drops from about 2.2µs to
  1.2µs (2 vCPU), with no added latency and 0 allocations; the handler
  contract is unchanged. Memory per reader becomes `Batch × MaxPacket`
  (about 520KB with the defaults); set `MaxPacket` for small datagrams.
  `Batch` > 1 where `recvmmsg` is unavailable is an error.

### Changed

- socket: remove `UnsupportedPolicy` and `UnsupportedIgnore`.
  Requested socket options now fail explicitly when unsupported; there is
  no silent fallback mode. **Breaking:** see Migration.
- tcp: admission slots are returned before a connection is counted as
  `Closed`, so a `Closed` count never runs ahead of the slots it frees.
- socket: options that do not fit the network are refused with
  `UnsupportedOptionError` instead of being silently ignored: any option on
  a network other than TCP/UDP, and FastOpen or KeepAlive on UDP.
  **Behaviour change:** a single `Options` with TCP-only settings can no
  longer be passed to UDP listeners.
- socket: `RecvBuffer` and `SendBuffer` are read back after being set.
  When the kernel grants less (Linux caps them at `net.core.rmem_max` /
  `wmem_max` without an error), Linux retries with `SO_RCVBUFFORCE` /
  `SO_SNDBUFFORCE` (needs CAP_NET_ADMIN), and a size that is still short
  returns the new `*socket.BufferSizeError`. `SetBuffers` verifies the same
  way. **Behaviour change:** a buffer larger than the host allows now fails
  Listen instead of being silently reduced; raise the sysctl.
- socket: `ReusePort` is supported on macOS (binds, does not balance load)
  and on FreeBSD via `SO_REUSEPORT_LB` (balanced).

### Migration

- Remove the `Unsupported` field from `FastOpenOptions`. Handle
  `UnsupportedOptionError` and omit Fast Open when the target platform or
  kernel does not support it.

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
