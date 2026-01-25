# Zero-Drift Release Protocol: Normative Invariants

This document defines the operatively binding engineering policy for xg2g.
Maintainer governance and CI enforcement must strictly adhere to these
invariants to guarantee that **Repository Truth = Runtime Truth**.

## 1. Truth Hierarchy (SSoT)

- **`VERSION`**: Canonical source for the SemVer tag (e.g., `3.1.5`).
- **`DIGESTS.lock`**: ONLY truth for digests (`@sha256:...`).
- **`RELEASE_MANIFEST.json`**: Canonical record of build state and metadata.

## 2. Documentation Drift Prevention (Docs-as-Code)

- **Templates as Source**: All docs (`README.md`), units (`xg2g.service`), and
  configurations (`docker-compose.yml`) MUST be generated from `templates/`.
- **No Direct Edits**: Direct modification of generated artifacts is PROHIBITED.
- **Idempotency**: `make docs-render` must be idempotent and order-stable.

## 3. "Stop-the-line" Governance

- **`make verify`**: The non-negotiable gate for all merges. Must be read-only.
- **Mandatory Success**: No changes merged unless `make verify` passes.

## 4. Release PR Firewall

- **Restricted Scope**: Release PRs must ONLY modify a restricted allowlist
  (SSoT Anchors and Generated Artifacts).
- **No Template Changes**: Templates must not be modified in Release PRs.

## 5. Reachability Guarantee

- **Digest Verification**: Before release, the digest MUST be proven reachable
  via `make release-verify-remote`.

---
**Status**: Operatively Binding Protocol
**Effective Date**: 2026-01-25
**Enforcement**: Mandatory CI Gate + Maintainer Review
