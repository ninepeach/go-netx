# go-netx

Reusable Go networking primitives: a small core, strong low-level building blocks, composed freely by applications.

The implemented packages include pooled buffers, buffered I/O, stream transfer,
endpoint metadata, socket controls, UDP helpers, and a reusable TCP server.

**Status:** seven-package core; see the [implementation status](docs/design/README.md).

Start at [docs/README.md](docs/README.md). The source and tests are authoritative for implemented API behavior. No standalone queue, flow, limit, lifecycle or statistics module is included.
