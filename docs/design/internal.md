# Internal packages — design

Overall design: [ARCHITECTURE.md](../../ARCHITECTURE.md). Packages under
`internal/` can change without breaking the public API; each exists because
more than one public package needs it or because it must stay hidden.

## internal/sys — platform syscalls for the transports

| API | Purpose |
|---|---|
| `MsgTrunc` | recvmsg flag for a truncated datagram (unix; 0 elsewhere) |
| `EnableDropCount(RawConn) bool`, `DropCount(oob) (uint32, bool)`, `OOBSize` | Linux `SO_RXQ_OVFL`: enable it, parse the count from control data |
| `ClassifyRead(error) ReadErr` | Transient, Truncated or Fatal, per platform |

- `DropCount` walks `cmsghdr` records with fixed offsets and `unsafe`, without
  allocating; it rejects truncated buffers and lengths that point outside
  the buffer. It parses kernel data, so it is fuzzed (NF-07).
- Read-error policy: on unix, `EINTR`, `EAGAIN`, `ENOBUFS`, `ENOMEM` and
  ICMP-driven errors (`ECONNREFUSED`, `EHOSTUNREACH`, `ENETUNREACH`) are
  transient; everything else is fatal. On Windows, `WSAEMSGSIZE` is
  truncation and `WSAECONNRESET`/`WSAENETRESET` (ICMP on an unconnected
  socket) are transient. The Windows policy is a pure function
  (`classifyWinsock`) compiled and tested on every platform.

Checkpoints: SYS-01…SYS-04, UDP-08, UDP-30, NF-07.

## internal/backoff — retry delay shared by TCP and UDP

`Next(d)`: 5ms first, doubling, capped at 1s (net/http's curve).
`Wait(d, stop)`: sleeps unless `stop` closes first. Both transports wait on
their shutdown channel, so Close never waits out a backoff (REG-02, REG-03).
Checkpoints: TCP-07/08, UDP-09/10.

## internal/counter — padded atomic counters

`Counter` is an `atomic.Uint64` padded to its own cache line (`Add`, `Inc`,
`Dec` for gauges, `Load`, `Store`, `Max`). Counters written by different
goroutines (per UDP reader, per socket) never share a line. Checkpoints:
SYS-04, NF-19.

## internal/leak — goroutine and file-descriptor leak check

`Take()` records goroutines and open FDs (Linux `/proc/self/fd`; -1
elsewhere); `Snapshot.Check(timeout, slack)` waits for both to return to
the baseline and reports a goroutine dump if they do not. It replaces goleak
to keep go-netx dependency-free. Used after shutdown (RUN-16) and in every
stress test. Checkpoint: NF-18.

## internal/tools/testdoc — test/document correspondence

Reads every test function's `// Checks:` directive (ignoring build tags) and
regenerates the test column of [docs/TESTING.md](../TESTING.md); its unit
test (DOC-01) fails `go test` on any mismatch, and DOC-02 requires a design
document for every package. Rules: [docs/CONTRIBUTING.md](../CONTRIBUTING.md).
