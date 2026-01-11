# internal/core - DEPRECATED

**Status:** This package is deprecated and scheduled for removal.

## Policy

**DO NOT add new code to `internal/core/`.**

This directory was originally intended as a catch-all for shared utilities, but has become ambiguous over time. New code should go to semantically appropriate locations:

- **HTTP client detection** → `internal/control/http/` or a new `internal/control/http/client/` package
- **Security utilities** (path sanitization, URL sanitization) → `internal/platform/security/` or directly in `internal/platform/fs/`
- **Media profile configuration** → `internal/pipeline/profiles/`
- **Domain-specific helpers** (Enigma2 URL conversion) → Domain-specific packages (e.g., `internal/enigma2/`)

## Existing Packages

| Package | Purpose | Suggested New Home |
|---------|---------|-------------------|
| `openwebif` | Enigma2 URL conversion | `internal/enigma2/urlconv` |
| `pathutil` | Path security (SecureJoin) | `internal/platform/fs/security` |
| `profile` | HLS profile configs | `internal/pipeline/profiles` |
| `urlutil` | URL sanitization for logging | `internal/platform/security` or `internal/log/urlutil` |
| `useragent` | HTTP client detection | `internal/control/http/client` |

## Migration Plan

Move packages incrementally when touched by feature work. Do not create a big-bang refactor PR.

## Rationale

The name "core" provides no semantic information about what belongs here vs. `internal/platform/`, leading to:
- Unclear import decisions for new code
- Drift toward "utils hell"
- Tight coupling as everything imports "core"

By dissolving `core/` into semantically clear locations, we enforce clearer layering and ownership.
