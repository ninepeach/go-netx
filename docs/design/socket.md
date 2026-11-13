# socket — implementation-aligned specification

## Scope

Configures a socket before it is used, through the standard library's control hooks. It owns no connection and no file descriptor.

Not here, and with no field or method reserved for it: transparent proxying (`IP_TRANSPARENT`, original destination, transparent reply), NAT, policy routing, connection tracking, socket lifecycle, reconnection. Those belong to the application. Keepalive, no-delay and buffer sizes stay with `net.TCPConn`, `net.UDPConn` and `net.Dialer`. A local address is `net.Dialer.LocalAddr` or the listen address, not an option here.

## API

| Symbol | Reason |
|---|---|
| `Options{Interface string; Mark *uint32; FastOpen, ReusePort bool}` | One declaration of what to set. `Mark` is a pointer so that 0 can be set; `false` leaves the OS default alone |
| `DialControl(o Options) func(ctx context.Context, network, address string, c syscall.RawConn) error` | Fits `net.Dialer.ControlContext` |
| `ListenControl(o Options) func(network, address string, c syscall.RawConn) error` | Fits `net.ListenConfig.Control` |

| Option | Sets | Consumer | If it cannot be set |
|---|---|---|---|
| `Interface` | The network interface the socket is tied to | A proxy's outbound, to keep its own traffic out of the tunnel | Error: unsupported, no such interface, no permission |
| `Mark` | Linux `SO_MARK` | The same, by policy routing | Error |
| `FastOpen` | TCP fast open, on a listener or a dialer | Ferry's listeners and dialer | Left off: it only saves a round trip |
| `ReusePort` | `SO_REUSEPORT` | Ferry's listeners | Error |

`NoFragment` and `BindAddressNoPort` are not in V1: nothing uses them yet ([P4](../ARCHITECTURE.md#principles)).

## Contract

**Failure is per option.** `Interface` and `Mark` change where traffic goes, and `ReusePort` decides whether a bind succeeds, so they never fall back ([Fallback](../ARCHITECTURE.md#fallback)). `FastOpen` is an optimization: where the platform or the kernel lacks it the socket simply goes without. There is no best-effort switch. An option the platform or the kernel does not have returns an error that wraps `errors.ErrUnsupported`. A request that is refused or invalid, such as no permission or no such interface, returns an error that wraps its cause and is **not** `errors.ErrUnsupported`, so a caller can tell a missing capability from a misconfiguration. Every such error names the option. `socket` defines no error of its own.

**Platforms.**

| Option | Linux | macOS | Windows |
|---|---|---|---|
| `Interface` | `SO_BINDTODEVICE` | `IP_BOUND_IF` or `IPV6_BOUND_IF`, by family | Unsupported |
| `Mark` | `SO_MARK` | Unsupported | Unsupported |
| `FastOpen` | `TCP_FASTOPEN` with a fixed queue on a listener, `TCP_FASTOPEN_CONNECT` on a dialer | `TCP_FASTOPEN` on a listener; off on a dialer | Off |
| `ReusePort` | `SO_REUSEPORT` | `SO_REUSEPORT` | Unsupported |

**Family and protocol.** The hook is called with the socket's own network (`tcp4`, `tcp6`, `udp4`, `udp6`), not the one passed to `Dial` or `Listen`; a dual-stack listener arrives as the IPv6 form. Options are applied by that family: no IPv6 option on an IPv4 socket and the reverse, no one Linux constant for every family. An option that does not apply to the protocol is an error, not a silent skip.

**RawConn.** Everything happens inside `RawConn.Control`. The `RawConn` and the descriptor are not kept, not closed, and not used for I/O. A failed `setsockopt` inside the callback is returned from the hook, even though `Control` itself succeeded.

**No rollback.** If the third option fails after two were applied, the hook fails, the dial or listen fails, and the standard library closes the socket. Applied options are not undone.

**[Ownership](../ARCHITECTURE.md#ownership), [Cancellation](../ARCHITECTURE.md#cancellation).** Nothing is owned and nothing blocks.

## Source-aligned details (Implemented)

The exported surface in `socket/socket.go` is `Options{Interface string; Mark *uint32; FastOpen, ReusePort bool}`, `DialControl(Options) func(context.Context,string,string,syscall.RawConn) error` and `ListenControl(Options) func(string,string,syscall.RawConn) error`. A non-nil `Mark` value is copied when constructing the hook; changing the original pointed-to value later does not change that hook. An all-zero `Options` returns from the hook without calling `RawConn.Control`. Unsupported network names return an error wrapping `errors.ErrUnsupported` for nonzero options; FastOpen on UDP is rejected before calling Control. For nonzero options, socket options are applied inside `RawConn.Control`; the callback error is returned separately from a Control invocation error. This module owns no descriptor and provides no socket Close.

KeepAlive and buffer-size convenience APIs are **not** exported by `socket/socket.go`; any future additions must be separately specified and must not be described as already implemented.

## Open questions

Additional socket options are future design work, not V1 commitments.

## Acceptance

- Each cell of the platform table: set and read back with `getsockopt`, or the unsupported error, or left off.
- Each failure row of the option table, including a failing `setsockopt` surfacing from the hook.
- `tcp4`, `tcp6`, `udp4`, `udp6` and a dual-stack listener.
- Zero `Options` touches nothing.
