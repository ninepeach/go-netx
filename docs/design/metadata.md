# metadata — endpoint and address design (V1)

## Scope

A small, dependency-free address value package for network endpoints that may contain an IP address or an unresolved domain name. It does **not** perform DNS resolution, dialing, routing, authentication, session tracking or packet I/O.

## Implemented API

```go
package metadata

import "net/netip"

type Endpoint struct {
    Addr   netip.Addr
    Port   uint16
    Domain string
}
```

Prefer Go standard-library types for already-resolved socket peers.

## Invariants

- A valid endpoint contains one well-formed host. An IP requires an empty domain field. A domain requires an invalid IP field. The zero value and conflicting field values are invalid.
- IPv4-mapped IPv6 is normalized with `Unmap()` on construction and parsing; IPv6 zones are preserved where supported.
- Port 0 is representable for binding or address construction, but callers must validate whether a destination port is required.
- Domain strings are not resolved, rewritten to IPs, or silently accepted as concrete `net.TCPAddr`/`net.UDPAddr`.
- String and parsing methods handle IPv4, bracketed IPv6 (including zones), and domain host:port. Invalid port syntax/range returns an error; do not truncate integers to `uint16`.
- No `unsafe` casting, reference-counted state, mutable shared buffers, or implicit network operations.

## Operations

| API | Contract |
|---|---|
| `ParseEndpoint(s)` | Parse an explicit host and decimal port in 0–65535. IPv6 literals use brackets. IPv4 and domains are unbracketed. |
| `FromDomain(domain, port)` | Construct an unresolved endpoint, preserving hostname spelling and an optional trailing dot. |
| `FromAddrPort(ap)`, `FromTCPAddr(addr)`, `FromUDPAddr(addr)` | Validate and normalize a concrete socket address. |
| `FromNetAddr(addr)` | Accept concrete TCP and UDP addresses. Other types wrap `errors.ErrUnsupported`. |
| `IsValid()`, `IsIP()`, `IsDomain()` | Classify endpoints only when their host is well formed and unambiguous. |
| `Normalize()` | Unmap valid mapped IPv4 values. Other values are unchanged. |
| `String()` | Return canonical host:port, or an empty string for an invalid value. |
| `AddrPort()`, `TCPAddr()`, `UDPAddr()` | Convert concrete IPs. Valid unresolved hosts return `ErrUnresolvedDomain`; malformed values return `ErrInvalidEndpoint`. |

Hostnames use ASCII letters, digits and internal hyphens in labels of 1–63 bytes, up to 253 bytes excluding an optional final dot. Callers supply ASCII IDNA labels for internationalized domains. Dotted all-numeric hosts must parse as IP literals. IPv6 zones are opaque strings preserved on native IPv6, with bracket delimiters and ASCII control characters rejected. Unmapping IPv4-mapped IPv6 removes the IPv6 zone.

## Package boundaries and migration

`metadata` depends only on the Go standard library. `udp` may use it for unresolved destinations at API boundaries, but existing `udp.Packet.Addr netip.AddrPort` remains unchanged in V1. `bufio` and `transfer` do not require `metadata` unless a concrete protocol-neutral use case arises. Do not introduce a top-level `network` package solely to host address types.

## Acceptance tests

- IPv4, IPv6, scoped IPv6, IPv4-mapped IPv6, domain and host:port round trips.
- Invalid hosts, missing host, missing/invalid ports, negative and >65535 ports.
- Conflicting IP + domain fields and zero-value endpoint.
- Unresolved domain conversion to socket addresses returns an error.
- Round trips with `netip.AddrPort`, `net.TCPAddr` and `net.UDPAddr`.
- Cross-platform compile and tests on supported Linux/macOS targets; Windows compile/fallback; no FreeBSD support commitment.

## Verification

Go 1.24.0 on macOS arm64 passed repository vet, tests, race tests and endpoint fuzzing. Metadata tests also ran on a Linux arm64 VM and Linux amd64 under QEMU user-mode. Windows amd64 was cross-compiled.

## Non-goals

DNS resolver, Dialer, proxy protocol serialization, application metadata, routing, packet ownership, NAT/session lifecycle.
