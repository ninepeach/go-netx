# Documentation

Each fact lives in exactly one document; the others link to it.

| Document | Owns |
|---|---|
| [ARCHITECTURE.md](ARCHITECTURE.md) | Goal, principles, module boundaries, dependencies, cross-module contracts, decisions |
| [CODING.md](CODING.md) | How code is written |
| [TESTING.md](TESTING.md) | How code is verified, across modules |
| [design/README.md](design/README.md) | Module status, the gate from design to code, state of the tree |
| [design/&lt;module&gt;.md](design/README.md) | Module API, implementation status, behavior, ownership, concurrency, platform constraints and acceptance |

**Implementation-aligned specification rule:** for claims marked **Implemented**, current source and tests are the evidence. Documentation must be corrected when it conflicts with code; do not silently change code to satisfy an outdated document. For **Designed** contracts, architecture decisions take precedence over module proposals. **Deferred** material is non-normative. Every exported API and observable behavior must be documented sufficiently for an independent compatible implementation.
