// Package metadata represents IP and unresolved domain endpoints.
package metadata

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

var (
	// ErrInvalidEndpoint indicates malformed input or an invalid Endpoint value.
	ErrInvalidEndpoint = errors.New("metadata: invalid endpoint")
	// ErrUnresolvedDomain indicates that a socket address requires an IP.
	ErrUnresolvedDomain = errors.New("metadata: unresolved domain")
)

// Endpoint holds exactly one host representation and a port. Its zero value
// is invalid. Constructors and ParseEndpoint return canonical IP addresses.
// Call IsValid when accepting a value assembled from exported fields.
type Endpoint struct {
	Addr   netip.Addr
	Port   uint16
	Domain string
}

// ParseEndpoint parses a numeric-port host:port pair without resolving it.
// IPv6 literals require brackets. Domains and IPv4 literals are unbracketed.
func ParseEndpoint(s string) (Endpoint, error) {
	host, portText, err := net.SplitHostPort(s)
	if err != nil {
		return Endpoint{}, fmt.Errorf("%w: %w", ErrInvalidEndpoint, err)
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		return Endpoint{}, fmt.Errorf("%w: port: %w", ErrInvalidEndpoint, err)
	}
	bracketed := strings.HasPrefix(s, "[")
	addr, err := netip.ParseAddr(host)
	if err == nil {
		if bracketed != addr.Is6() {
			return Endpoint{}, fmt.Errorf("%w: invalid IP brackets", ErrInvalidEndpoint)
		}
		return FromAddrPort(netip.AddrPortFrom(addr, uint16(port)))
	}
	if bracketed {
		return Endpoint{}, fmt.Errorf("%w: invalid IPv6 literal: %w", ErrInvalidEndpoint, err)
	}
	return FromDomain(host, uint16(port))
}

// FromAddrPort constructs an IP endpoint and normalizes mapped IPv4.
func FromAddrPort(ap netip.AddrPort) (Endpoint, error) {
	e := Endpoint{Addr: ap.Addr(), Port: ap.Port()}
	if !e.IsIP() {
		return Endpoint{}, ErrInvalidEndpoint
	}
	return e.Normalize(), nil
}

// FromDomain constructs an unresolved ASCII DNS hostname endpoint.
func FromDomain(domain string, port uint16) (Endpoint, error) {
	e := Endpoint{Domain: domain, Port: port}
	if !e.IsDomain() {
		return Endpoint{}, fmt.Errorf("%w: invalid domain %q", ErrInvalidEndpoint, domain)
	}
	return e, nil
}

// IsValid reports whether e has exactly one well-formed host.
func (e Endpoint) IsValid() bool { return e.IsIP() || e.IsDomain() }

// IsIP reports whether e is a valid IP endpoint.
func (e Endpoint) IsIP() bool { return e.Addr.IsValid() && e.Domain == "" && validZone(e.Addr.Zone()) }

// IsDomain reports whether e is a valid unresolved domain endpoint.
func (e Endpoint) IsDomain() bool { return !e.Addr.IsValid() && validDomain(e.Domain) }

// Normalize canonicalizes IPv4-mapped IPv6. Other values are returned as is.
func (e Endpoint) Normalize() Endpoint {
	if e.IsIP() {
		e.Addr = e.Addr.Unmap()
	}
	return e
}

// String returns host:port in canonical form, or an empty string for an
// invalid value.
func (e Endpoint) String() string {
	if e.IsIP() {
		return netip.AddrPortFrom(e.Addr.Unmap(), e.Port).String()
	}
	if e.IsDomain() {
		return net.JoinHostPort(e.Domain, strconv.Itoa(int(e.Port)))
	}
	return ""
}

func validZone(zone string) bool {
	for _, c := range zone {
		if c < ' ' || c == '\x7f' || c == '[' || c == ']' {
			return false
		}
	}
	return true
}

func validDomain(domain string) bool {
	name := strings.TrimSuffix(domain, ".")
	if len(name) == 0 || len(name) > 253 {
		return false
	}
	// A dotted all-numeric host is treated as an IP literal, even if malformed.
	numeric := true
	for {
		label, rest, more := strings.Cut(name, ".")
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := range len(label) {
			c := label[i]
			if c >= '0' && c <= '9' {
				continue
			}
			numeric = false
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-') {
				return false
			}
		}
		if !more {
			break
		}
		name = rest
	}
	return !numeric || !strings.Contains(domain, ".")
}
