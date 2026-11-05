// Package netx is the runtime of go-netx: it starts services, stops them on
// a signal, and shuts them down in three phases (stop accepting, drain,
// abort).
//
// The transports live in subpackages: tcp and udp move bytes, socket
// configures sockets, flow gives connections an identity, limit admits work,
// queue bounds backlogs and buf owns memory. Protocols, authentication,
// sessions and routing live above go-netx. See ARCHITECTURE.md.
package netx
