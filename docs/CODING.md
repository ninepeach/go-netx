# Coding requirements

Redis style: terse, efficient, to the point. Implement what matters most; trim everything else that can be trimmed. These rules apply to every module.

## Public API

1. Every exported symbol is listed in its module document with the reason it exists. No reason, no export.
2. No convenience wrapper for what a caller can write in one line with existing API. No two ways to do one thing.
3. No option, hook or counter without a consumer that needs it ([P4](ARCHITECTURE.md#principles)).

## Code

4. Few files per package, flat. No interface with a single implementation, no layer that only forwards.
5. The steady-state hot path does not allocate. Any performance claim is backed by a benchmark ([TESTING.md](TESTING.md)).
6. Programmer errors (bad bounds, negative sizes, misuse of ownership) panic. Anything that comes from the network or from a caller-supplied `io.Reader`/`io.Writer` is untrusted: validate it and return an error.
7. Few sentinel errors, each one a caller can act on.
8. Comments state the contract: ownership, lifetime, errors, concurrency. No history, ticket numbers or narration.
9. `gofmt` and `go vet` clean.

## Tests

10. Tests cover the contract and the acceptance list in the module document, nothing else. Deleted API takes its tests with it.
11. No `time.Sleep` before an assertion: wait on a condition with a timeout. Listen on port 0.

## Process

12. Work on `main`; no feature branches.
13. Code follows the gate in [design/README.md](design/README.md).
14. Imported old code is raw material. It is cut down to these rules and to the module document, not preserved.
