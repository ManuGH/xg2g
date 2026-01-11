# scripts/ - Internal Build Helpers

**⚠️ DO NOT call these scripts directly.**

These are **internal implementation details** called by the [Makefile](../Makefile).
They are NOT guaranteed to be stable or usable standalone.

## Usage

Use the Makefile instead:

```bash
make build          # Uses scripts/build.sh internally
make test           # Uses scripts/test-wrapper.sh internally
make generate       # Uses scripts/generate.sh internally
make docker-build   # Uses scripts/docker-build.sh internally
```

## Why Not Call Directly?

1. **Missing context**: Scripts assume environment variables set by Makefile
2. **No validation**: Makefile validates prerequisites (Go version, dependencies)
3. **Fragile paths**: Scripts use relative paths assuming `make` working directory
4. **Breaking changes**: Script arguments may change without notice

## Exception: CI Workflows

GitHub Actions may call scripts directly **only when**:
- Makefile invocation is not possible (e.g., conditional logic, multi-stage pipelines)
- Versions are pinned (Go toolchain, dependencies)
- Full environment context is explicitly provided

This is **not a blanket exception** - prefer `make` in CI whenever possible.

---

**If you need a standalone command, open an issue to discuss adding it to `cmd/`.**
