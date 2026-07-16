# netx

`netx` is a reusable Go server runtime. It manages startup, serving, operating
system signals, errors, and graceful shutdown while leaving protocol and domain
behavior to the caller.

## Quick start

Override the lifecycle functions and start the loop:

```go
s := netx.NewServer()
s.OnServe = serve
s.OnShutdown = shutdown

if err := s.Loop(); err != nil {
	log.Fatal(err)
}
```

The callbacks receive a context:

```go
func serve(ctx context.Context) error
func shutdown(ctx context.Context) error
```

`Loop()` listens for `SIGINT` and `SIGTERM`. Use `LoopContext(ctx)` when the
parent process already controls cancellation.

## Service interface

Reusable services implement two methods:

```go
type Service interface {
	Serve(context.Context) error
	Shutdown(context.Context) error
}
```

Starting one requires two lines:

```go
s := netx.NewServer(service)
return s.Loop()
```

Optional hooks can extend the lifecycle without changing the service:

```go
s := netx.NewServer(service)

s.OnStart = func(context.Context) error {
	log.Print("started")
	return nil
}
s.OnStop = func(context.Context) error {
	log.Print("stopped")
	return nil
}
s.OnError = func(err error) {
	log.Printf("server: %v", err)
}

return s.Loop()
```

## Existing functions

`ServiceFuncs` adapts existing functions without requiring a new type:

```go
s := netx.NewServer(netx.ServiceFuncs{
	ServeFunc:    serve,
	ShutdownFunc: shutdown,
})

return s.Loop()
```

## TCP service

The TCP server exposes overridable functions directly:

```go
s := netx.NewTCPServer(":9000", netx.TCPServerOptions{
	MaxConnections: 4096,
})
s.OnConnect = func(_ context.Context, conn net.Conn) error {
	_, err := io.Copy(conn, conn)
	return err
}

return s.Loop()
```

TCP Fast Open can be enabled through socket options:

```go
netx.TCPServerOptions{
	Socket: socket.Options{
		FastOpen: socket.FastOpenOptions{
			Enabled:     true,
			Backlog:     256,
			Unsupported: socket.UnsupportedError,
		},
	},
}
```

Linux supports TCP Fast Open for listeners and outbound sockets. Other
platforms can return `socket.UnsupportedOptionError` or ignore the feature,
depending on the selected policy.

## UDP service

The UDP server follows the same object model. `OnPacket` returns the response
datagram; returning `nil, nil` sends nothing.

```go
s := netx.NewUDPServer(":9001", netx.UDPServerOptions{
	MaxHandlers: 256,
})
s.OnPacket = func(_ context.Context, request netx.UDPRequest) ([]byte, error) {
	return request.Payload, nil
}

return s.Loop()
```

## Lifecycle behavior

A `Server` is one-shot and follows:

```text
new → starting → running → stopping → stopped
```

- The default graceful shutdown timeout is 30 seconds.
- `Stop()` requests asynchronous cancellation.
- `Shutdown(ctx)` requests cancellation and waits for completion.
- Serve and shutdown errors are preserved with `errors.Join`.
- Hook panics are converted to errors.
- Calling `Loop` more than once returns `ErrAlreadyRunning`.

## Examples

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

## Packages

- `netx`: service lifecycle and overridable TCP/UDP servers.
- `tcp`: TCP listener, connection lifecycle, limits, and dialer.
- `udp`: packet server, bounded handlers, response writer, and client.
- `socket`: socket configuration and Linux TCP Fast Open.
- `mux`: implementation-neutral session and stream contracts.

## Verification

```sh
make test
make race
make vet

# Communication-focused tests
go test -race ./integration ./tcp ./udp
```
