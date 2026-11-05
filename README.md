# go-netx

Minimal, high-performance TCP and UDP foundation for Go gateways: no
per-packet goroutines, no per-packet allocation, no internal queues, no
dependencies. See [ARCHITECTURE.md](ARCHITECTURE.md) for the design and its
frozen rules, and [docs/TESTING.md](docs/TESTING.md) for the test checklist.

```
netx    runtime: start, signals, stop accepting → drain → abort
tcp     accept loop, admission, Conn IDs, Dialer
udp     multi-reader datagram transport
socket  socket options (ReusePort, buffers, TCP Fast Open)
flow    flow IDs, close state machine, idle-expiring table
limit   admission limiters and budgets
queue   bounded ring with overflow policy
buf     pooled buffers with explicit ownership
```

## TCP

```go
ln, err := tcp.Listen(ctx, "tcp", ":9000", func(ctx context.Context, c *tcp.Conn) {
	_, _ = io.Copy(c, c) // the server closes c when the handler returns
}, tcp.Options{Admit: limit.NewConcurrency(10_000)})
if err != nil {
	log.Fatal(err)
}
log.Fatal(netx.NewServer(ln).Loop()) // SIGINT/SIGTERM → graceful shutdown
```

Long-lived handlers watch `c.Draining()` to move clients away during a
graceful shutdown; `ctx` is cancelled only when the drain times out.

## UDP

```go
conn, err := udp.Listen(ctx, "udp", ":9001", func(c *udp.Conn, p *udp.Packet) {
	_ = c.Reply(p, p.Data) // p.Data is borrowed: valid only in this call
}, udp.Options{Readers: 4, Socket: socket.Options{ReusePort: true, RecvBuffer: 8 << 20}})
if err != nil {
	log.Fatal(err)
}
log.Fatal(netx.NewServer(conn).Loop())
```

The handler runs inline on its reader. To keep data or hand it to another
goroutine, copy it into an owned buffer:

```go
b := pool.Copy(p.Data)       // owned
if !q.Push(b) {              // transfer failed: still ours
	b.Release()
}
```

## Several services

```go
netx.NewServer(netx.Group(tcpServer, udpConn, netx.ServiceFuncs{ServeFunc: flows.Run})).Loop()
```

## Observability

Every component has `Stats()` returning a snapshot (`udp.Stats.KernelDrops`,
`tcp.Stats.Rejected`, `queue.Stats.Dropped`, …). Export them however you like.

## Performance

UDP echo round trip on loopback (2 vCPU, Go 1.24, 512-byte payload):

| | ns/op | allocs/op |
|---|---|---|
| v0.2 | 32,500 | 5 |
| v1.0 | 10,800 | 0 |

## Examples

```sh
make examples
./build/examples/tcp-echo     # tcp-client, udp-echo, udp-client,
                              # http-server, socks5-server
```

## Verification

```sh
make check   # test, race (with and without -tags netxdebug), vet
```

Before pushing, run `make verify` (gofmt, vet, doc check, race with and
without `netxdebug`, cross-compile). Contribution and testing rules are in
[docs/CONTRIBUTING.md](docs/CONTRIBUTING.md); the per-module test matrix,
regression list and a general checklist for network libraries are in
[docs/TESTING.md](docs/TESTING.md).
