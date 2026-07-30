# Zero-Drift Release Protocol: Normative Invariants

This document defines the operatively binding engineering policy for xg2g.
Maintainer governance and CI enforcement must strictly adhere to these
invariants to guarantee a verifiable chain from source to published artifact.

## 1. Truth Hierarchy (SSoT)

- **`backend/VERSION`**: Canonical source intent for the release tag (for
  example, `v3.9.2`).
- **GitHub immutable release attestation**: Canonical identity for the tag,
  source commit, and published file digests.
- **OCI registry digest + GitHub/Sigstore attestations**: Canonical identity and
  provenance for the published container manifest.
- **`DIGESTS.lock`**: Optional deployment-pin registry. A `pending` entry is
  preparation state, never proof that an image exists.
- **`RELEASE_MANIFEST.json`**: Checked-in release intent only. Fields that are
  unknowable before the release commit/build remain `null`; it must not invent
  provenance.

## 2. Documentation Drift Prevention (Docs-as-Code)

- **Templates as Source**: All docs (`README.md`), units (`infra/systemd/xg2g.service`), and
  configurations (`infra/systemd/docker-compose.yml`) MUST be generated from `templates/`.
- **No Direct Edits**: Direct modification of generated artifacts is PROHIBITED.
- **Idempotency**: `make docs-render` must be idempotent and order-stable.

## 3. "Stop-the-line" Governance

- **`make verify`**: The non-negotiable gate for all merges. Must be read-only.
- **Mandatory Success**: No changes merged unless `make verify` passes.

## 4. Release PR Firewall

- **Restricted Scope**: Release PRs must ONLY modify a restricted allowlist
  (SSoT Anchors and Generated Artifacts).
- **No Template Changes**: Templates must not be modified in Release PRs.

## 5. Draft-First Reachability Guarantee

- GoReleaser MUST create a draft and upload all files before publication.
- The version tag and `latest` MUST resolve to the same OCI digest.
- The OCI index MUST contain `linux/amd64` and `linux/arm64`.
- Release files and the OCI manifest MUST be attested before the draft is
  published.
- Any failed verification leaves the GitHub release as a draft.
- Published releases and their associated tags/assets MUST be immutable.

---
**Status**: Operatively Binding Protocol
**Effective Date**: 2026-01-25
**Enforcement**: Mandatory CI Gate + Maintainer Review
