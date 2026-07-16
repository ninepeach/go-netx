# netx

Reusable Go networking primitives for TCP/UDP services and multiplexed streams.

```go
srv, err := tcp.Listen(ctx, "tcp", ":9000", tcp.HandlerFunc(handler), tcp.ListenOptions{})
if err != nil { return err }
return srv.Serve(ctx)
```

The server owns accepted TCP connections and closes each connection after its
handler returns. Protocol parsing, authentication, routing, and proxy behavior
belong in the handler.

TCP Fast Open is currently implemented on Linux. Other platforms either return
`socket.UnsupportedOptionError` or ignore the option according to the selected
policy.

## Packages

- `tcp`: listener lifecycle, connection ownership, limits, and outbound dialer.
- `udp`: packet loop, bounded handler concurrency, and response writer.
- `socket`: portable socket configuration and Linux TCP Fast Open support.
- `mux`: implementation-neutral session and stream contracts.

## Examples

```sh
go run ./examples/tcp-echo
go run ./examples/udp-echo
```

The `mux` package intentionally defines contracts rather than a wire protocol.
Adapters for yamux, smux, QUIC, or a Ferry-specific protocol can implement the
interfaces without coupling the core server to one framing format.

## Verification

```sh
make test
make race
make vet
```
