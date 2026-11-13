# TCP server — implemented contract

## Scope

Package `tcp` provides a reusable accept loop, bounded admission, connection
identity and Stop Accept → Drain → Abort. It has no dialer, framing, relay,
authentication, routing, signal handling or logger dependency.

## API

```go
type Handler func(context.Context, *Conn)
type Options struct {
    Socket         socket.Options
    MaxConnections int
    MaxPerIP       int
    IPv6PrefixBits int
}
func Listen(context.Context, string, string, Handler, Options) (*Server, error)
func NewServer(net.Listener, Handler, Options) (*Server, error)
func (*Server) Serve(context.Context) error
func (*Server) Shutdown(context.Context) error
func (*Server) Close() error
func (*Server) Addr() net.Addr
func (*Server) Draining() <-chan struct{}
func (*Server) Stats() Stats
func (*Conn) ID() uint64
func (*Conn) Draining() <-chan struct{}
```

`Conn` wraps the accepted connection only to carry identity and tracking. It declares `UnwrapReader` and `UnwrapWriter`, so `bufio.Writer` keeps `writev`, and `transfer` keeps read-wait, splice and the standard library's kernel copy, when given a `*tcp.Conn`.

`Listen` creates the listener with `socket.ListenControl`; `NewServer`
takes ownership of an existing listener. Negative limits and invalid IPv6 prefix
sizes return errors. Zero limits are unlimited. IPv6 per-IP admission groups by
/64 by default; 128 counts exact addresses. IPv4 is counted by address.

The server owns accepted connections and closes each after its handler returns.
Handlers run concurrently. Their context is cancelled only on abort; draining is
reported separately. The handler must return to complete a graceful drain.

`Shutdown` closes the listener and starts draining. If its context expires, it
cancels handlers, closes all active connections, and returns the context error.
`Close` aborts immediately. `Serve` waits for all handlers. A permanent
accept error aborts active handlers and is returned; temporary errors use bounded,
interruptible exponential backoff.

`Serve` is one-shot. Shutdown before Serve is valid and makes the first Serve
return nil without accepting. Further Serve calls return `ErrClosed`.
Admission and shutdown are serialized so no connection is tracked after drain
begins.

`Conn` forwards `CloseRead`, `CloseWrite`, `ReadFrom` and `WriteTo`
without unwrapping application wrappers. Missing half-close returns
`errors.ErrUnsupported` and never substitutes a full close.

## Verification

Tests cover real-socket echo, unique IDs, counters, global/per-IP admission,
graceful drain, deadline abort, connection close, and shutdown before Serve.
The macOS unit, race and vet gates passed on 2026-11-12. Linux, Windows and load
gates remain required before release.
