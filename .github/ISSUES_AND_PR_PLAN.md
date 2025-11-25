# 📋 GitHub Issues & PR Plan

This document outlines the structured approach to upstreaming the technical fixes and improvements identified in the audit.

## 1. Proposed Issues

### Issue 1: [High Priority] Fix Excessive FFI Buffer Allocation

**Title**: `perf: reduce FFI output buffer allocation factor`
**Description**:
The current FFI implementation allocates an output buffer `100x` the size of the input buffer to handle potential transcoding expansion. This is excessive for AC3->AAC transcoding (which typically expands by a factor of ~4x or less) and poses a significant OOM risk under load.
**Task**:

- Change allocation factor from `100` to `4`.
- Verify with FFI tests.

### Issue 2: [High Priority] Implement Rust Error Propagation

**Title**: `feat: implement xg2g_last_error and propagate messages to Go`
**Description**:
Currently, Rust FFI errors return generic error codes (e.g., `-1`) to Go, making debugging difficult. The `xg2g_last_error` function is defined but not implemented.
**Task**:

- Implement `thread_local` error storage in Rust.
- Implement `xg2g_last_error` to return the stored error string.
- Update Go wrapper to consume and log these errors.

### Issue 3: [Security] Enable Non-Root User by Default

**Title**: `sec: re-enable non-root Docker user by default with opt-out`
**Description**:
The `Dockerfile` currently runs as `root` by default, contradicting the documentation and security best practices. The non-root user setup is commented out.
**Task**:

- Uncomment `USER xg2g:xg2g`.
- Add documentation for LXC users on how to opt-out (set `user: root`).

### Issue 4: [Docs] Update Architecture Documentation

**Title**: `docs: update ARCHITECTURE.md to match implementation`
**Description**:
`ARCHITECTURE.md` references `gorilla/mux`, but the codebase uses `go-chi/chi`. It also claims non-root containers are used, which was previously false (fixed in Issue 3).
**Task**:

- Update dependency references.
- Verify other architectural claims.

## 2. Pull Request Strategy

We will group these changes into logical PRs to ensure easy review.

| PR | Type | Scope | Dependencies |
|----|------|-------|--------------|
| **#1** | `fix` | **FFI Performance & Reliability**<br>- Reduces buffer allocation (Issue 1)<br>- Implements error propagation (Issue 2) | None |
| **#2** | `sec` | **Docker Security**<br>- Enables non-root user (Issue 3) | None |
| **#3** | `docs` | **Documentation Update**<br>- Updates `ARCHITECTURE.md` (Issue 4) | None |

## 3. Verification

All PRs must pass:

- `make lint`
- `make test`
- `make test-ffi` (for PR #1)
- `make docker-build` (for PR #2)
