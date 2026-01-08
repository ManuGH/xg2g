# SOA Boundaries & Architecture Rules

This document defines the strict Service Oriented Architecture (SOA) boundaries for the `xg2g` repository.
These rules are enforced by CI.

## 1. Domain Map

| Domain | Responsibility | Owner | Allowed Outbound Deps |
| :--- | :--- | :--- | :--- |
| `platform` | OS abstractions, FS, Network, Process management | Infra | NONE (Leaf) |
| `observability` | Logging, Metrics, Tracing | Infra | `platform` |
| `config` | Configuration parsing & validation | Core | `platform` |
| `lease` | Hardware resource management (Tuners, Transcoders) | Core | `platform`, `observability` |
| `metadata` | Static data (Channels, EPG, Recordings DB) | Feature | `platform`, `observability`, `config` |
| `playback` | Media pipeline (FFmpeg), HLS Serving, VOD | Feature | `metadata`, `lease`, `platform`, `observability` |
| `session` | Orchestration Layer (Lifecycle Management) | Feature | `playback`, `metadata`, `lease`, `platform` |
| `control` | Public API, Auth, Ingress (HTTP Handlers) | API | `session`, `metadata`, `playback` |

## 2. Import Rules (Strict)

1. **No Circular Dependencies**: Domains must form a Directed Acyclic Graph (DAG).
2. **Layers**:
    - **Layer 1 (Foundation)**: `platform`, `observability`, `config`
    - **Layer 2 (Core Logic)**: `lease`, `metadata`, `playback`
    - **Layer 3 (Orchestration)**: `session`
      (Canonical: `internal/domain/session`)
    - **Layer 4 (Ingress)**: `control`, `cmd`
3. **Prohibitions**:
    - `playback` MUST NOT import `session` or `control`.
    - `metadata` MUST NOT import `session` (DB shouldn't know about runtime
      active sessions).
    - `platform` MUST NOT import anything domain-specific.

## 3. Directory Structure

- `internal/domain/<name>`: The **ONLY** place where domain logic lives.
- `cmd/<name>`: Wiring only. No logic.
- `api/<version>`: Contracts (OpenAPI) only. No logic.

## 4. Legacy Code (Strangler Pattern)

- Legacy code resides in `internal/pipeline`, `internal/api` (old locations).
- **Rule**: Legacy code MAY import new `internal/domain/*` packages.
- **Rule**: New `internal/domain/*` packages MUST NOT import legacy code.
  - *Exception*: See `docs/arch/SOA_MIGRATION_DEBT.md` for strictly managed allow-lists.
- **Goal**: Move code from legacy to domain until legacy is empty.
