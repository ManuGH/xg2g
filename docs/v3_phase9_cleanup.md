# Phase 9-3: Cleanup & Garbage Collection Walkthrough

## Overview

This phase implements robust lifecycle cleanup for HLS sessions to prevent disk exhaustion and store bloat. It handles both "friendly" termination and "orphaned" artifacts left by crashes or restart loops.

## Cleanup Mechanisms

### 1. On-Stop Cleanup (Best Effort)

When a session transitions to a terminal state (`STOPPED`, `FAILED`, `CANCELLED`) via `Orchestrator.handleStart` (which manages the lifecycle):

- The `cleanupFiles(sessionID)` helper is triggered immediately.
- This removes `<HLSRoot>/sessions/<sessionID>` recursively.
- This covers normal stops and explicit cancellations.

### 2. Background Store Sweeper

A periodic background goroutine (`Sweeper`) scans the `StateStore`:

- **Criteria**: Checks for sessions where `State.IsTerminal()` AND `UpdatedAt < Now - SessionRetention`.
- **Action**: Deletes the session record from the store AND triggers filesystem cleanup again (safety net).

### 3. Orphan Filesystem Sweeper

The `Sweeper` also scans the `<HLSRoot>/sessions/` directory:

- **Criteria**:
    1. Directory name matches `^[a-zA-Z0-9_\-]+$` (Safety Check).
    2. Directory modification time is older than `FileRetention` (defaulting to `SessionRetention`).
    3. Session ID is **not found** in the active `StateStore` (via `GetSession`).
- **Action**: Recursively deletes the orphan directory.

## Safety & Security

- **Path Confinement**: All file operations are rooted at `<HLSRoot>/sessions/`.
- **ID Validation**: `safeIDRe` strictly enforces alphanumeric/dash/underscore session IDs to prevent path traversal.
- **Race Protection**: Orphan cleanup only targets directories older than retention period, preventing race conditions with newly created sessions.

## Configuration

- `XG2G_V3_SWEEP_INTERVAL` (or `Sweeper.Interval`): How often to run. Default: **5 minutes**.
- `SessionRetention`: How long to keep terminal session records. Default: **24 hours**.
- `FileRetention`: How long to keep orphan files. Defaults to `SessionRetention` if 0.

## Operational Note

If the sweeper is disabled or fails, disk usage may grow. Monitoring should track `xg2g_store_ops_total{op="delete_session"}` (implied) or logs `sweep store removed expired sessions`.
