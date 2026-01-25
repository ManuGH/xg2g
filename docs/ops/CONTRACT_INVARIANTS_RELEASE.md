# Zero-Drift Release Protocol: Normative Invariants

This document defines the operatively binding engineering policy for xg2g releases. Maintainer governance and CI enforcement must strictly adhere to these invariants to guarantee that **Repository Truth = Runtime Truth**.

## 1. Truth Hierarchy (SSoT)

- **`VERSION`**: The canonical source for the SemVer tag (e.g., `3.1.5`).
- **`DIGESTS.lock`**: The ONLY source of truth for container digests (`@sha256:...`).
- **`RELEASE_MANIFEST.json`**: The canonical record of build state (git SHA, timestamp, environment).

## 2. Documentation Drift Prevention (Docs-as-Code)

- **Templates as Source**: All documentation (`README.md`), systemd units (`xg2g.service`), and Docker Compose configurations (`docker-compose.yml`) MUST be generated from templates in `templates/`.
- **No Direct Edits**: Direct modification of generated artifacts is PROHIBITED.
- **Idempotency**: The rendering process (`make docs-render`) must be idempotent and order-stable.

## 3. "Stop-the-line" Governance

- **`make verify`**: This target is the non-negotiable gate for all PRs and merges. It must run in a read-only mode (no writes/side-effects).
- **Mandatory Success**: No code or documentation changes shall be merged unless `make verify` passes in a clean environment.

## 4. Release PR Firewall

- **Restricted Scope**: Pull Requests identified as "Release PRs" (via Title, Branch, or Label) must ONLY modify a restricted allowlist of files (SSoT Anchors and Generated Artifacts).
- **Prohibited Template Changes**: Release PRs must not modify templates; functional documentation changes must happen in feature PRs.

## 5. Reachability Guarantee

- **Digest Verification**: Before a release is finalized, the digest in `DIGESTS.lock` must be proven reachable in the remote registry (`ghcr.io/manugh/xg2g`) using `make release-verify-remote`.

---
**Status**: Operatively Binding Protocol
**Effective Date**: 2026-01-25
**Enforcement**: Mandatory CI Gate + Maintainer Review
