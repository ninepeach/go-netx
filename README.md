# go-netx

Reusable Go networking primitives: a small core, strong low-level building blocks, composed freely by applications.

The implemented packages include pooled buffers, buffered I/O, stream transfer,
endpoint metadata, socket controls, UDP helpers, and a reusable TCP server.

**Status:** being redesigned module by module; see the [status table](docs/design/README.md).

Start at [docs/README.md](docs/README.md). Documentation distinguishes **Implemented**, **Designed** and **Deferred**; existing source remains the evidence for implemented API behavior. `queue.Ring`, `limit`, `pipe`, `observable` and `memory` are excluded from V1.
