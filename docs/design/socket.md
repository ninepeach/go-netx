# Package socket — design

Overall design: [ARCHITECTURE.md](../../ARCHITECTURE.md).

## 1. Scope

| Owns | Does not own |
|---|---|
| Socket options applied before bind/connect | Server or connection lifecycles |
| Platform differences and the unsupported-option policy | Choosing values for a workload |
| Building `net.ListenConfig` and `net.Dialer` | Accepting, reading, writing |

`socket` imports nothing from go-netx; `tcp` and `udp` use it.

## 2. API

```go
type Options struct {
    FastOpen   FastOpenOptions // TCP only; Enabled, Backlog, Unsupported policy
    ReusePort  bool            // Linux
    RecvBuffer, SendBuffer int // SO_RCVBUF / SO_SNDBUF when > 0
    KeepAlive  KeepAlive       // Disable, Idle, Interval, Count; zero = Go default
}
func ListenConfig(Options) net.ListenConfig
func Dialer(base net.Dialer, Options) net.Dialer // returns a copy
func SetBuffers(BufferSetter, Options) error     // for sockets made elsewhere
func IsUnsupported(error) bool                   // *UnsupportedOptionError
type UnsupportedPolicy uint8                     // UnsupportedError, UnsupportedIgnore
```

## 3. Design

**Everything before bind.** Options are applied in the `Control` hook of
`net.ListenConfig` / `net.Dialer`, before the socket binds or connects.
That is what makes them work: a TCP listener's buffer sizes and keep-alive
are inherited by accepted connections (no per-connection syscalls), and
TCP window scaling is negotiated with the larger buffer already in place.

**Keep-alive** maps to Go's own fields: `Disable` sets `KeepAlive = -1`; any
other non-zero setting becomes `net.KeepAliveConfig{Enable: true, …}`, and
fields left at zero keep Go's defaults. The zero value changes nothing
(Go's default: enabled, 15s). The listener's setting reaches accepted
connections because Go applies `ListenConfig.KeepAliveConfig` on accept.

**Unsupported options.** A requested feature the platform lacks is an
explicit `*UnsupportedOptionError` (FastOpen and ReusePort off Linux;
buffer sizes off unix/windows), never a silent no-op.
`UnsupportedIgnore` applies only to errors that mean "not supported"
(`ENOPROTOOPT`, `EOPNOTSUPP` on Linux); any other setsockopt error is
returned unchanged (REG-01).

Options that make no sense for a socket type are skipped: FastOpen for UDP.

## 4. Platform notes

| Option | Linux | macOS / BSD | Windows |
|---|---|---|---|
| RecvBuffer / SendBuffer | ✓ (kernel doubles the value) | ✓ | ✓ |
| KeepAlive idle/interval/count | ✓ | ✓ (Go's support) | ✓ (Go's support) |
| ReusePort | ✓ | unsupported error | unsupported error |
| FastOpen | ✓ | policy | policy |

## 5. Decisions

- One `Options` struct shared by TCP and UDP, so a gateway configures
  sockets the same way everywhere.
- ReusePort is reported unsupported off Linux even where `SO_REUSEPORT`
  exists (macOS does not load-balance with it), rather than appearing to
  work and silently not spreading load.

## 6. Testing

SOCK-01…SOCK-16 in [docs/TESTING.md](../TESTING.md). Option effects are
verified with `getsockopt` on Linux (and macOS for buffer inheritance).

## 7. Open items

- PROXY protocol, if gateways run behind L4 balancers that hide the source
  address (see ARCHITECTURE.md).
