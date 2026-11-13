# Testing

This file defines the required test gates, **not a claim that they have passed**. Do not reuse go-netx-old results as evidence for this repository. Record actual commands, platforms and outcomes before marking any capability verified.

What to test for a module is the Acceptance section of its [module document](design/README.md). This document covers what is common to all modules.

## Gate

A module is **implemented** when, for its package:

1. `gofmt -l` prints nothing and `go vet` passes.
2. `go test` and `go test -race` pass on Linux and macOS.
3. It compiles for Windows (`GOOS=windows go vet`).
4. Every Acceptance item in its module document has a test.
5. Each performance claim in its module document has a benchmark recorded with ns/op, B/op and allocs/op.

FreeBSD is outside V1.

## Benchmarks

Record the machine, Go version and command with the numbers. Compare against go-netx-old or sing only with a setup someone else can reproduce ([P5](ARCHITECTURE.md#principles)).

## Fuzzing

Cursor arithmetic and anything that parses untrusted input gets a fuzz target checked against a simple model.

## Specification reconstruction gate

For each **Implemented** public symbol, independently verify documentation against source and tests: exact Go declaration; zero value/defaults; panic/error/EOF/partial-progress behavior; buffer and socket ownership; cancellation and blocking; concurrency safety; platform build tags; statistics count boundaries; and performance claims. A developer using only docs must be able to implement observably equivalent behavior. Any undocumented branch is a documentation defect, not permission to invent semantics.

Cross-package lifecycle assertions should follow the implemented [TCP](design/tcp.md) and [transfer](design/transfer.md) contracts. Test assertions must distinguish successful half-close from full abort, buffer Release (panics twice) from socket Close (idempotent where specified), and single-peer NAT address adaptation from a non-existent association manager.
