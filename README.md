# netx

`netx` provides a reusable service lifecycle, high-level TCP/UDP servers, and
lower-level networking packages for applications that need complete control.

## Service lifecycle

`netx.Server` manages startup, serving, signals, errors, and graceful shutdown.
Override only the functions needed by the service:

```go
s := netx.NewServer()
s.OnServe = serve
s.OnShutdown = shutdown
return s.Loop()
```

The callback signatures are:

```go
func serve(context.Context) error
func shutdown(context.Context) error
```

For reusable implementations, satisfy the native lifecycle interface:

```go
type Service interface {
	Serve(context.Context) error
	Shutdown(context.Context) error
}

s := netx.NewServer(service)
s.OnStart = func(context.Context) error {
	log.Print("service started")
	return nil
}
s.OnStop = func(context.Context) error {
	log.Print("service stopped")
	return nil
}
return s.Loop()
```

Use `LoopContext(ctx)` when a parent application already manages signals. A
Server is one-shot and follows `new → starting → running → stopping → stopped`.
The default graceful shutdown timeout is 30 seconds.

Existing functions can also use an adapter:

```go
s := netx.NewServer(netx.ServiceFuncs{
	ServeFunc: serve,
	ShutdownFunc: shutdown,
})
return s.Loop()
```

## Network servers

The lifecycle API is independent of protocol implementations. The following
helpers remain available for small standalone TCP and UDP servers.

### TCP server: one call

```go
package main

import (
	"context"
	"io"
	"net"

	"github.com/ninepeach/netx"
)

func main() {
	_ = netx.ListenAndServeTCP(context.Background(), ":9000",
		func(_ context.Context, conn net.Conn) error {
			_, err := io.Copy(conn, conn)
			return err
		})
}
```

The high-level server automatically handles listening, the accept loop,
connection ownership, handler goroutines, cancellation, and graceful shutdown.

### UDP server: request in, response out

```go
package main

import (
	"context"

	"github.com/ninepeach/netx"
)

func main() {
	_ = netx.ListenAndServeUDP(context.Background(), ":9001",
		func(_ context.Context, request netx.UDPRequest) ([]byte, error) {
			return request.Payload, nil
		})
}
```

Returning `nil, nil` processes a datagram without sending a response.

### Production options

```go
err := netx.ListenAndServeTCP(ctx, ":9000", handler,
	netx.TCPServerOptions{
		MaxConnections:  4096,
		ShutdownTimeout: 10 * time.Second,
		OnError:         func(err error) { log.Printf("connection: %v", err) },
		Socket: socket.Options{
			FastOpen: socket.FastOpenOptions{
				Enabled:     true,
				Backlog:     256,
				Unsupported: socket.UnsupportedError,
			},
		},
	})
```

Linux supports TCP Fast Open for listeners and outbound sockets. Other
platforms can return `socket.UnsupportedOptionError` or ignore the feature,
depending on `Unsupported` policy.

### Start first, serve separately

Use the constructors when the application needs the bound address or controls
its own goroutines:

```go
server, err := netx.NewTCPServer(ctx, "127.0.0.1:0", handler, options)
if err != nil { return err }
log.Printf("listening on %s", server.Addr())
return server.Serve(ctx)
```

Equivalent `NewUDPServer` is available for UDP.

## Client examples

Run a server:

```sh
go run ./examples/tcp-echo
go run ./examples/udp-echo
```

Run the matching client in another terminal:

```sh
go run ./examples/tcp-client "hello TCP"
go run ./examples/udp-client "hello UDP"
```

## Advanced packages

- `tcp`: listener lifecycle, connection ownership, limits, and dialer.
- `udp`: packet server, bounded handlers, response writer, and client.
- `socket`: socket configuration and Linux TCP Fast Open.
- `mux`: implementation-neutral session and stream contracts.

Protocol parsing, authentication, routing, and other domain behavior
intentionally remain above the server layer.

## Verification

The integration suite starts real loopback servers and verifies concurrent TCP
clients, repeated messages, UDP request/response, cancellation, and shutdown.

```sh
make test
make race
make vet

# Communication-focused suite
go test -race ./integration ./tcp ./udp
```
