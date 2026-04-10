# Operational Lifecycle Contract

Normative lifecycle contract for `xg2g` public deployments.

This block defines how installation, startup, upgrade, restore, and rollback are
evaluated before the system is treated as operationally valid.

## Rules

1. Lifecycle checks must read one effective truth: loaded config plus verified runtime state.
2. Preflight, drift reporting, support diagnostics, and future restore/rollback gates must not create a second config source.
3. Findings are classified as:
   - `fatal`: process or lifecycle action must not proceed
   - `block`: lifecycle action may continue only in degraded/not-ready mode; operator attention is required
   - `warn`: contract is still operable but risky or non-durable
4. Public deployment and public exposure security contracts are part of lifecycle preflight, not separate optional checks.

## Shared Preflight Kernel

`xg2g preflight` is the operator entrypoint for the shared lifecycle preflight engine.

Current first-slice coverage:

- data directory, store path, HLS root, and recording root writability
- API listen address, Enigma2 base URL, and TLS file readability
- engine dependency availability
- public deployment contract evaluation
- public exposure security contract evaluation
- persistence-risk warnings such as memory-backed state or temp-backed data paths

The daemon startup path uses the same engine via `health.PerformStartupChecks`.
This means startup and operator preflight already consume one shared contract model.

## Runtime Truth Snapshot

Deliverable 2 extends the same preflight engine with an optional runtime snapshot.
The snapshot is a verified runtime derivation, not a second config source.

Current runtime-truth coverage:

- installed systemd unit vs canonical host copy vs repo deploy bundle
- resolved compose stack, including `COMPOSE_FILE` overrides and optional GPU/NVIDIA overlays
- compose image truth, env-file truth, and mounted path coverage for `dataDir`, store, and HLS roots
- env-shape only (keys, not secret values), including required-key presence and selected compose files
- build metadata plus lightweight state markers such as `drift_state.json` version and SQLite `user_version`

Runtime drift is classified as:

- `supported`
- `drifted_but_allowed`
- `unsupported`
- `blocking`

When a runtime snapshot is attached, its findings are folded back into the same
preflight report under the `runtime_truth` contract. This keeps startup,
operator CLI, and future support/upgrade/restore/rollback surfaces on one
shared lifecycle truth.

## Upgrade / Migration Gates

Deliverable 3 extends the same preflight engine with upgrade-specific gates.
Upgrade checks are not a separate validator; they are folded into the shared
report under the `upgrade_migration_contract`.

Current upgrade-slice coverage:

- current runtime release derived from the live compose image
- target release derived from `--target-version`, repo deploy bundle image, or current binary release
- release delta classification, including downgrade blocking and already-current warnings
- raw file-config migration assessment against `config.V3ConfigVersion`
- deprecated YAML paths and deprecated/legacy runtime env keys as upgrade blockers
- SQLite state-schema compatibility checks based on the supported schema versions of the target binary

Current state-schema policy:

- older schema than target binary: `warn` (`migration_required`)
- newer schema than target binary: `block` (`forward_incompatible`)
- exact schema match: `current`

## Backup / Restore Gates

Deliverable 4 starts the restore contract on the same shared preflight engine.
Restore checks are folded into the shared report under the
`backup_restore_contract`.

Current restore-slice coverage:

- restore artifact inventory derived from the storage inventory SSOT, not from ad hoc restore scripts
- minimal restore-set classification between required state and optional carried-forward state
- runtime-secret preconditions based on verified runtime env truth
- restore artifact integrity checks for SQLite and JSON backup media
- restore state-schema compatibility checks against the current binary before a restore is treated as valid

Current restore policy:

- missing required restore artifacts: `block`
- missing optional carried-forward artifacts such as `channels.json`: `warn`
- unreadable or corrupt restore media: `fatal`
- restore schema newer than the current binary supports: `fatal`
- restore schema older than the current binary supports: `warn` (`migration_required`)

## CLI

```bash
xg2g preflight --config /etc/xg2g/config.yaml --operation=startup
xg2g preflight --config /etc/xg2g/config.yaml --operation=upgrade --json
xg2g preflight --config /etc/xg2g/config.yaml --operation=upgrade --runtime-snapshot --target-version v3.4.6 --json
xg2g preflight --config /etc/xg2g/config.yaml --operation=restore --runtime-snapshot --restore-root /var/backups/xg2g/2026-04-10 --json
xg2g preflight --config /etc/xg2g/config.yaml --runtime-snapshot --json
```

Exit codes:

- `0`: `ok` or `warn`
- `1`: `block`
- `2`: `fatal` or config-load failure

## Operation Scope

The current slice provides the common preflight kernel for:

- `startup`
- `install`
- `upgrade`
- `restore`
- `rollback`

Operation-specific gates such as migration compatibility, backup completeness,
and rollback span rules are follow-on deliverables and must extend this same
shared engine instead of introducing parallel validators.
