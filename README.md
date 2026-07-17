# netx

`netx` provides small, overridable TCP and UDP servers with built-in signal
handling and graceful shutdown.

## TCP server

Copy this complete example to `main.go`:

```go
package main

import (
	"context"
	"io"
	"log"
	"net"

	"github.com/ninepeach/go-netx"
)

func main() {
	s := netx.NewTCPServer(":9000")

	s.OnConnect = func(_ context.Context, conn net.Conn) error {
		_, err := io.Copy(conn, conn)
		return err
	}

	s.OnStart = func(context.Context) error {
		log.Printf("TCP echo listening on %s", s.Addr())
		return nil
	}

	if err := s.Loop(); err != nil {
		log.Fatal(err)
	}
}
```

Run it with `go run main.go`, then connect with `nc 127.0.0.1 9000`.
Everything typed into `nc` is echoed back. Press `Ctrl-C` in the server
terminal for graceful shutdown.

`Loop()` listens for `SIGINT` and `SIGTERM`, closes the listener, and waits for
active connections. The server owns each accepted connection and closes it
after `OnConnect` returns.

Options are passed to the constructor:

```go
s := netx.NewTCPServer(":9000", netx.TCPServerOptions{
	MaxConnections:  4096,
	ShutdownTimeout: 10 * time.Second,
})
```

Use `OnOpen` and `OnClose` for protocol-independent connection setup and
observability. Returning an error from `OnOpen` rejects the connection before
the handler runs:

```go
s := netx.NewTCPServer(":9000", netx.TCPServerOptions{
	OnOpen: func(_ context.Context, conn net.Conn) error {
		return conn.SetDeadline(time.Now().Add(10 * time.Second))
	},
	OnClose: func(conn net.Conn) {
		log.Printf("closed %s", conn.RemoteAddr())
	},
})
```

## UDP server

```go
s := netx.NewUDPServer(":9001")

s.OnPacket = func(_ context.Context, request netx.UDPRequest) ([]byte, error) {
	return request.Payload, nil
}

if err := s.Loop(); err != nil {
	log.Fatal(err)
}
```

The returned bytes are sent to the packet source. Return `nil, nil` to send no
response. Use `UDPServerOptions.MaxHandlers` to limit concurrent handlers.
UDP listeners now accept the same `socket.Options` family as TCP listeners,
including `ReusePort` on supported platforms.

## Lifecycle hooks

TCP and UDP servers expose the same lifecycle hooks:

```go
s.OnStart = func(context.Context) error {
	log.Printf("listening on %s", s.Addr())
	return nil
}
s.OnStop = func(context.Context) error {
	log.Print("stopped")
	return nil
}
s.OnError = func(err error) {
	log.Printf("server: %v", err)
}
```

- `Loop()` manages `SIGINT` and `SIGTERM`.
- `LoopContext(ctx)` uses caller-provided cancellation.
- `Stop()` requests asynchronous cancellation.
- `Shutdown(ctx)` cancels and waits for completion.
- `Ready()` is closed after network binding succeeds or fails.
- `Addr()` returns the bound address after `Ready()`.

A server is one-shot: `new → starting → running → stopping → stopped`.

## Generic services

Non-network services can use the same lifecycle runtime:

```go
type Service interface {
	Serve(context.Context) error
	Shutdown(context.Context) error
}

return netx.NewServer(service).Loop()
```

Functions can be assigned directly:

```go
s := netx.NewServer()
s.OnServe = serve
s.OnShutdown = shutdown
return s.Loop()
```

## TCP Fast Open

```go
s := netx.NewTCPServer(":9000", netx.TCPServerOptions{
	Socket: socket.Options{
		FastOpen: socket.FastOpenOptions{
			Enabled:     true,
			Backlog:     256,
			Unsupported: socket.UnsupportedError,
		},
	},
})
```

TCP Fast Open is implemented on Linux. Other platforms return an unsupported
option error or ignore it according to the selected policy.

## Echo examples

Build all example executables:

```sh
make examples
./build/examples/tcp-echo
```

The generated binaries are placed in `build/examples/`:

- `tcp-echo`, `tcp-client`
- `udp-echo`, `udp-client`
- `http-server`
- `socks5-server`

Run the minimal HTTP server:

```sh
./build/examples/http-server
curl http://127.0.0.1:8080/hello
```

Run the minimal no-auth SOCKS5 CONNECT server:

```sh
./build/examples/socks5-server
curl --socks5-hostname 127.0.0.1:1080 https://example.com
```

The SOCKS5 example intentionally implements only the CONNECT command. Protocol
logic remains in the example rather than the transport library.

Start a server:

```sh
go run ./examples/tcp-echo
# or
go run ./examples/udp-echo
```

From another terminal:

```sh
go run ./examples/tcp-client "hello TCP"
# or
go run ./examples/udp-client "hello UDP"
```

## Packages

- `netx`: overridable servers and service lifecycle.
- `tcp`: TCP listener, connection lifecycle, limits, and dialer.
- `udp`: context-aware packet server, socket options, response writer, and client.
- `socket`: socket options and Linux TCP Fast Open.
- `mux`: session and stream contracts.

## Verification

```sh
make test
make race
make vet
```
