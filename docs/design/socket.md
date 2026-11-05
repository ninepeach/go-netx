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
    ReusePort  bool            // Linux, FreeBSD (balanced); macOS (no balancing)
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
explicit `*UnsupportedOptionError`, never a silent no-op: FastOpen off
Linux, ReusePort outside Linux/FreeBSD/macOS, buffer sizes outside
unix/Windows. `UnsupportedIgnore` (FastOpen only, kept by D-13) applies
only to errors that mean "not supported" (`ENOPROTOOPT`, `EOPNOTSUPP` on
Linux); any other setsockopt error is returned unchanged (REG-01).

**Options that do not fit the network are refused (D-14).** Any option on a
network other than TCP or UDP (for example `unix`), and the TCP-only
options (FastOpen, KeepAlive) on UDP, return `*UnsupportedOptionError` with
`Platform` set to `"network <name>"`. This is a configuration error, so
`UnsupportedIgnore` does not cover it. The zero `Options` works on every
network. A gateway configuring TCP and UDP therefore uses two `Options`
values when it sets TCP-only options.

**ReusePort per platform (D-15).** Linux uses `SO_REUSEPORT` and FreeBSD
`SO_REUSEPORT_LB` (12+); both spread connections and datagrams across the
sockets. macOS accepts `SO_REUSEPORT` but does not spread load: one socket
receives everything. It is supported so a development Mac runs the
production configuration; do not expect several UDP readers to share work
there.

## 4. Platform notes

| Option | Linux | macOS / BSD | Windows |
|---|---|---|---|
| RecvBuffer / SendBuffer | ✓ (kernel doubles the value) | ✓ | ✓ |
| KeepAlive idle/interval/count | ✓ | ✓ (Go's support) | ✓ (Go's support) |
| ReusePort | ✓ balanced | macOS ✓ not balanced; FreeBSD ✓ balanced (`SO_REUSEPORT_LB`); other BSDs unsupported error | unsupported error |
| FastOpen | ✓ | policy | policy |

## 5. Decisions

- One `Options` type for TCP and UDP, so a gateway configures sockets the
  same way everywhere; a value that sets TCP-only options is for TCP only
  (D-14).
- D-13: keep `UnsupportedIgnore` for FastOpen.
- D-14: options that do not fit the network are errors.
- D-15: ReusePort on macOS (documented as unbalanced) and FreeBSD (balanced).

## 6. Testing

SOCK-01…SOCK-21 in [docs/TESTING.md](../TESTING.md). Option effects are
verified with `getsockopt` on Linux (and macOS for buffer inheritance).
SOCK-20/21 (macOS, FreeBSD reuse-port) are written but have only been
compiled so far.

## 7. Open items

- PROXY protocol, if gateways run behind L4 balancers that hide the source
  address (see ARCHITECTURE.md).
