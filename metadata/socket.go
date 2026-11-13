package metadata

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
)

// FromTCPAddr constructs an IP endpoint from a non-nil concrete TCP address.
func FromTCPAddr(addr *net.TCPAddr) (Endpoint, error) {
	if addr == nil {
		return Endpoint{}, ErrInvalidEndpoint
	}
	return fromSocketAddr(addr.IP, addr.Port, addr.Zone)
}

// FromUDPAddr constructs an IP endpoint from a non-nil concrete UDP address.
func FromUDPAddr(addr *net.UDPAddr) (Endpoint, error) {
	if addr == nil {
		return Endpoint{}, ErrInvalidEndpoint
	}
	return fromSocketAddr(addr.IP, addr.Port, addr.Zone)
}

// FromNetAddr accepts TCP and UDP address types. Other types return an
// error wrapping errors.ErrUnsupported.
func FromNetAddr(addr net.Addr) (Endpoint, error) {
	switch a := addr.(type) {
	case *net.TCPAddr:
		return FromTCPAddr(a)
	case *net.UDPAddr:
		return FromUDPAddr(a)
	default:
		return Endpoint{}, fmt.Errorf("metadata: %T: %w", addr, errors.ErrUnsupported)
	}
}

func fromSocketAddr(ip net.IP, port int, zone string) (Endpoint, error) {
	if port < 0 || port > 65535 {
		return Endpoint{}, fmt.Errorf("%w: port %d out of range", ErrInvalidEndpoint, port)
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok || zone != "" && !addr.Is6() {
		return Endpoint{}, ErrInvalidEndpoint
	}
	return FromAddrPort(netip.AddrPortFrom(addr.WithZone(zone), uint16(port)))
}

// AddrPort converts a valid IP endpoint. A valid domain returns
// ErrUnresolvedDomain.
func (e Endpoint) AddrPort() (netip.AddrPort, error) {
	if e.IsIP() {
		return netip.AddrPortFrom(e.Addr.Unmap(), e.Port), nil
	}
	if e.IsDomain() {
		return netip.AddrPort{}, ErrUnresolvedDomain
	}
	return netip.AddrPort{}, ErrInvalidEndpoint
}

// TCPAddr converts a valid IP endpoint to a new TCP address.
func (e Endpoint) TCPAddr() (*net.TCPAddr, error) {
	ap, err := e.AddrPort()
	if err != nil {
		return nil, err
	}
	return net.TCPAddrFromAddrPort(ap), nil
}

// UDPAddr converts a valid IP endpoint to a new UDP address.
func (e Endpoint) UDPAddr() (*net.UDPAddr, error) {
	ap, err := e.AddrPort()
	if err != nil {
		return nil, err
	}
	return net.UDPAddrFromAddrPort(ap), nil
}
