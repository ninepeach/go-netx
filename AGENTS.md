# Instructions for AI agents

Read and follow [docs/CONTRIBUTING.md](docs/CONTRIBUTING.md) before changing anything in this repository.

The short version:

- Run `make verify` before every push. It must pass.
- Every Test/Benchmark/Fuzz function needs a `// Checks: <ID>` directive naming checkpoints in [docs/TESTING.md](docs/TESTING.md). After adding or changing tests, update the checkpoint status and run `make testdoc`. Never hand-edit the 测试 column.
- Never narrow test coverage by adding build tags to an existing test file, deleting tests or weakening assertions.
- Test-only branches must not change non-test code. Register bugs you find as 🔧 in docs/TESTING.md instead.
- Package layout and dependency rules in [ARCHITECTURE.md](ARCHITECTURE.md) are frozen.
