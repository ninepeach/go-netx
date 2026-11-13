//go:build darwin || (linux && (amd64 || arm64))

package udp

import (
	"encoding/binary"
	"net"
	"net/netip"
	"strconv"
	"syscall"
	"unsafe"
)

func batchPeer(c *net.UDPConn, name *syscall.RawSockaddrAny) (netip.AddrPort, error) {
	if remote := c.RemoteAddr(); remote != nil {
		return normalize(remote.(*net.UDPAddr).AddrPort()), nil
	}
	return decodeName(name)
}

func decodeName(name *syscall.RawSockaddrAny) (netip.AddrPort, error) {
	switch name.Addr.Family {
	case syscall.AF_INET:
		addr := (*syscall.RawSockaddrInet4)(unsafe.Pointer(name))
		return netip.AddrPortFrom(netip.AddrFrom4(addr.Addr), decodePort(&addr.Port)), nil
	case syscall.AF_INET6:
		addr := (*syscall.RawSockaddrInet6)(unsafe.Pointer(name))
		ip := netip.AddrFrom16(addr.Addr).Unmap()
		if ip.Is6() && addr.Scope_id != 0 {
			zone := strconv.FormatUint(uint64(addr.Scope_id), 10)
			if iface, err := net.InterfaceByIndex(int(addr.Scope_id)); err == nil {
				zone = iface.Name
			}
			ip = ip.WithZone(zone)
		}
		return netip.AddrPortFrom(ip, decodePort(&addr.Port)), nil
	}
	return netip.AddrPort{}, syscall.EAFNOSUPPORT
}

func decodePort(p *uint16) uint16 {
	return binary.BigEndian.Uint16(unsafe.Slice((*byte)(unsafe.Pointer(p)), 2))
}
