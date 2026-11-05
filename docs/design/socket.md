# Package socket — design

Overall design: [ARCHITECTURE.md](../../ARCHITECTURE.md).

## 1. Scope

| Owns | Does not own |
|---|---|
| Socket options applied before bind/connect | Server or connection lifecycles |
| Platform differences and explicit unsupported-option errors | Choosing values for a workload |
| Building `net.ListenConfig` and `net.Dialer` | Accepting, reading, writing |

`socket` imports nothing from go-netx; `tcp` and `udp` use it.

## 2. API

```go
type Options struct {
    FastOpen   FastOpenOptions // TCP only; unsupported always returns an error
    ReusePort  bool            // Linux, macOS, FreeBSD
    RecvBuffer, SendBuffer int // SO_RCVBUF / SO_SNDBUF when > 0
    KeepAlive  KeepAlive       // Disable, Idle, Interval, Count; zero = Go default
}
type FastOpenOptions struct { Enabled bool; Backlog int }
func ListenConfig(Options) net.ListenConfig
func Dialer(base net.Dialer, Options) net.Dialer // returns a copy
func SetBuffers(BufferSetter, Options) error     // for sockets made elsewhere
func IsUnsupported(error) bool                   // *UnsupportedOptionError
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

**Fail fast.** A requested option either succeeds or returns an error.
Unsupported platform/kernel features return `*UnsupportedOptionError`;
other syscall errors are propagated unchanged. There is no ignore/fallback
policy. Options on non-TCP/UDP networks are rejected, as are TCP-only
options (Fast Open and KeepAlive) on UDP.

**ReusePort.** Linux uses `SO_REUSEPORT`; FreeBSD uses
`SO_REUSEPORT_LB` to distribute connections and datagrams. macOS accepts
`SO_REUSEPORT` and permits multiple binds, but does not distribute load
across sockets.

## 4. Platform notes

| Option | Linux | macOS | FreeBSD | Windows |
|---|---|---|---|---|
| RecvBuffer / SendBuffer | ✓ | ✓ | ✓ | ✓ |
| KeepAlive idle/interval/count | ✓ | ✓ | ✓ | ✓ |
| ReusePort | ✓ | ✓ (bind only) | ✓ (load-balanced) | unsupported error |
| FastOpen | kernel-dependent | unsupported error | unsupported error | unsupported error |

## 5. Decisions

- One `Options` struct shared by TCP and UDP, so a gateway configures
  sockets the same way everywhere.
- Unsupported options are never silently ignored. Callers must explicitly
  omit an option that is unavailable on the target platform.
- macOS ReusePort is accepted for configuration parity, but callers must
  not assume load balancing; use Linux or FreeBSD when per-socket load
  distribution is required.

## 6. Testing

SOCK-01…SOCK-21 in [docs/TESTING.md](../TESTING.md). Platform-specific
options are verified with `getsockopt` and real bind attempts.

## 7. Open items

- PROXY protocol, if gateways run behind L4 balancers that hide the source
  address (see ARCHITECTURE.md).
