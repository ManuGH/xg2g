# ADR-020: Storage Strategy 2026 - SQLite as Durable Truth

## Status

Proposed (Phase 2.1)

## Context

xg2g currently suffers from "Storage Sprawl," utilizing four different
database engines (SQLite, BoltDB, BadgerDB, and semi-structured JSON files)
across various modules. This complexity increases maintenance overhead.

## Decision

1. **SQLite as Single Durable Truth**: SQLite (via `modernc.org/sqlite`)
   is the formal system-of-record for all persistent state.
2. **Redis as Optional Cache**: Redis remains supported only as an
   ephemeral cache or distributed lock provider.
3. **De-escalation of Bolt/Badger**: BoltDB/BadgerDB are deprecated.
   All modules will migrate to SQLite.
4. **Shadow Policy**: "Shadow" means unreachable in production runtime.
   Shadow implementations MUST NOT perform Dual-Writes to durable media
   during the transition phase.

## Durable Entities (SQLite)

- **Library Index**: Already SQLite. Maintains recording metadata.
- **Session Records**: State-of-record for active/historical playback.
- **Leases & Idempotency**: Atomic synchronization primitives.
- **Resume States**: User progress per recording.
- **Capabilities**: Probed service metadata (formerly JSON).

## Ephemeral Entities (Memory/Redis)

- **EPG Data**: Derived from source; can be regenerated.
- **Picon Mappings**: Cached for UI performance.

## Implementation Requirements

- **Journaling**: Must use `WAL` mode for concurrent read/write.
- **Synchronous**: Set to `NORMAL` in WAL mode.
- **Busy Timeout**: Mandatory `5000ms`.
- **Versioning**: Schema versioning via `PRAGMA user_version`.

## Consequences

- **Positive**: Simplified backup, unified transactions, smaller binary.
- **Negative**: Migration overhead for existing BoltDB users.
- **Operator-Grade Migration**: Migration MUST be restart-safe, idempotent,
  and verifiable via checksums or record counts.
