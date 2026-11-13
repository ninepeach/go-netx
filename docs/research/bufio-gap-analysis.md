# Network I/O gap analysis

Reviewed the reference dev snapshot at `27b7377` alongside the existing Reader, Writer, UDP datagram contract, and Go standard-library copy paths.

| Reference files and interfaces | Finding | go-netx action |
|---|---|---|
| `io.go`, `conn.go`, `common/network/conn.go` | Buffer-specific I/O avoids an extra allocation. Its write API consumes buffers and does not carry an accepted byte count. | First round: borrow buffers and return counts, including on error. |
| `cache.go`, `append.go`, `addr_conn.go`, `fallback.go` | Prefix replay, directional overrides and address projection are separate capabilities. Cached buffers there use reference counting. | First round: one owner, explicit adapter composition, no implicit upstream conversion. |
| `wait.go`, `copy_direct_posix.go`, `copy_direct_windows.go`, `common/network/direct.go` | Poll readiness before allocating. Platform-specific receive operations and replaceable wrappers affect safety. | First round: a bare Unix TCP readiness adapter and ordinary I/O fallback. |
| `vectorised.go`, `vectorised_unix.go`, `vectorised_windows.go` | Gather writes reduce calls, but progress and platform support matter. | First round: use standard socket gather writes and one explicit borrowed-vector interface. |
| `copy.go`, `copy_session.go`, `copy_direct.go`, `splice_linux.go` | Transfer needs capability selection, accepted-byte accounting and cancellation. A syscall path must own partial progress. | First round: synchronous transfer with standard copy capabilities; defer dedicated splice and vectorised transfer. |
| `packet_batch*.go`, `bind*.go`, `nat*.go`, `counter_packet_conn.go` | Native batches differ in syscall layout and partial progress; address adapters must preserve packet boundaries. | First round: share packet interfaces only. Native batch and Bind/NAT remain later UDP work. |
| `chunk.go`, `buffer.go`, `only.go` | Chunk sizing and direction restrictions can be expressed without an independent scheduler or queue. | Keep adaptive Reader and explicit flush. Callers compose directional views. |
