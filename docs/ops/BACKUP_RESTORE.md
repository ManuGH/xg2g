# Storage Operations: Backup, Restore, and Verify

`xg2g` does not persist state in one place.

- Store-backed SQLite state lives under `XG2G_STORE_PATH`.
- File-backed product and operator state lives under `XG2G_DATA_DIR` / `XG2G_DATA`.
- Generated outputs such as playlist, XMLTV, and picons are reconstructable and should not be treated as primary backup targets.

## State Inventory

### Primary durable state

These artifacts should be preserved across restart, upgrade, and restore.

| Class | Path | Backup | Verify | Purpose |
| :--- | :--- | :--- | :--- | :--- |
| SQLite | `$XG2G_STORE_PATH/sessions.sqlite` | Yes | Yes | Active and historical playback session state |
| SQLite | `$XG2G_STORE_PATH/deviceauth.sqlite` | Yes | Yes | DeviceAuth grants, pairings, and device binding state |
| SQLite | `$XG2G_STORE_PATH/resume.sqlite` | Yes | Yes | Recording resume positions |
| SQLite | `$XG2G_STORE_PATH/capabilities.sqlite` | Yes | Yes | Source and receiver capability scan truth |
| SQLite | `$XG2G_STORE_PATH/decision_audit.sqlite` | Yes | Yes | Playback decision history |
| SQLite | `$XG2G_STORE_PATH/capability_registry.sqlite` | Yes | Yes | Observed host/device/source capability registry |
| SQLite | `$XG2G_STORE_PATH/entitlements.sqlite` | Yes | Yes | Purchased and granted playback entitlements |
| SQLite | `$XG2G_STORE_PATH/household.sqlite` | Yes | Yes | Household profile and unlock state |
| JSON | `$XG2G_DATA_DIR/channels.json` | Yes | Yes, if present | Persisted channel enable/disable state |
| JSON | `$XG2G_DATA_DIR/series_rules.json` | Yes | Yes, if present | Persisted DVR series rules |

### Operational state

These files are useful operational truth, but are not primary product state.

| Class | Path | Backup | Verify | Purpose |
| :--- | :--- | :--- | :--- | :--- |
| JSON | `$XG2G_DATA_DIR/drift_state.json` | Optional | Yes, if present | Last verification drift snapshot |
| JSON | `$XG2G_STORE_PATH/last_sweep.json` | Optional | Yes, if present | `storage decision-sweep` diff baseline |

### HLS artifact classes

These artifacts live under `XG2G_HLS_ROOT` and are intentionally not treated as authoritative product state.

| Class | Path | Backup | Verify | Purpose |
| :--- | :--- | :--- | :--- | :--- |
| Transient dir | `$XG2G_HLS_ROOT/sessions/` | No | No | Ephemeral live session playlists, segments, and first-frame markers |
| Materialized dir | `$XG2G_HLS_ROOT/recordings/` | No | No | Materialized recording HLS artifacts with eviction and rehydration semantics |

### Reconstructable outputs

These are runtime or generated artifacts. They are usually better rebuilt than backed up.

| Class | Path | Backup | Verify | Purpose |
| :--- | :--- | :--- | :--- | :--- |
| Generated file | `$XG2G_DATA_DIR/$XG2G_PLAYLIST_FILENAME` | No | No | Generated playlist output |
| Generated file | `$XG2G_DATA_DIR/$XG2G_XMLTV` | No | No | Generated XMLTV output |
| Cache dir | `$XG2G_DATA_DIR/picons/` | No | No | Downloaded picon cache |
| SQLite | `library.db_path` from `config.yaml` | Optional | Yes, if configured | Rebuildable library scan and duration cache |

## Backup Strategy

Always take a backup before upgrading `xg2g`.

### 1. Online SQLite backup

This is the safest way to snapshot live SQLite state while the daemon is running.

```bash
sqlite3 "$XG2G_STORE_PATH/sessions.sqlite" ".backup '/var/backups/xg2g/sessions-$(date +%F).sqlite'"
```

### 2. Online SQLite backup (compacted)

Produces a smaller, defragmented copy with `VACUUM INTO`.

> [!IMPORTANT]
> This requires free disk space roughly equal to the database size. If it fails midway, delete the partial output manually.

```bash
sqlite3 "$XG2G_STORE_PATH/sessions.sqlite" "VACUUM INTO '/var/backups/xg2g/sessions-compact.sqlite'"
```

### 3. Cold backup

1. Stop `xg2g`.
2. Use `.backup` or copy the main `.sqlite` file together with its `-wal` and `-shm` side files.
3. Copy the file-backed state from `XG2G_DATA_DIR` that you want to preserve.

## Restore Strategy

1. Stop `xg2g`.
2. Restore the target `.sqlite` files under `XG2G_STORE_PATH`.
3. Remove stale `-wal` and `-shm` side files if you replaced the main `.sqlite` file from backup media.
4. Restore `channels.json` and `series_rules.json` under `XG2G_DATA_DIR` when you want user-visible state carried forward.
5. Start `xg2g`. Built-in migrations will handle structural SQLite updates.

Before starting the daemon, assess the backup set against the current runtime
and public/security contracts:

```bash
xg2g preflight \
  --config /etc/xg2g/config.yaml \
  --operation=restore \
  --runtime-snapshot \
  --restore-root /var/backups/xg2g/2026-04-10 \
  --json
```

## Integrity Verification

### Built-in tool

`xg2g storage verify --all` verifies:

- all known SQLite state under `XG2G_STORE_PATH`
- `library.db_path` from `config.yaml` when library mode is enabled
- `channels.json`, `series_rules.json`, `drift_state.json`, and `last_sweep.json` when present
- it intentionally does not verify `sessions/` or `recordings/` under `XG2G_HLS_ROOT`, because those are runtime/materialized artifacts rather than authoritative state

```bash
# Routine health pass
xg2g storage verify --all --mode quick

# Deep diagnosis of one SQLite file
xg2g storage verify --path "$XG2G_STORE_PATH/sessions.sqlite" --mode full

# Validate one JSON state file
xg2g storage verify --path "$XG2G_DATA_DIR/channels.json"
```

### Manual SQLite verification

```bash
sqlite3 "$XG2G_STORE_PATH/sessions.sqlite" "PRAGMA quick_check;"
sqlite3 "$XG2G_STORE_PATH/sessions.sqlite" "PRAGMA integrity_check;"
```

If the output is `ok`, the SQLite file is structurally sound.

## Automation Examples

### Backup all primary durable state

```bash
DBS=(
  "sessions.sqlite"
  "deviceauth.sqlite"
  "resume.sqlite"
  "capabilities.sqlite"
  "decision_audit.sqlite"
  "capability_registry.sqlite"
  "entitlements.sqlite"
  "household.sqlite"
)
BACKUP_DIR="/var/backups/xg2g/$(date +%F)"
mkdir -p "$BACKUP_DIR"

for DB in "${DBS[@]}"; do
  sqlite3 "$XG2G_STORE_PATH/$DB" ".backup '$BACKUP_DIR/$DB'"
done

for FILE in channels.json series_rules.json; do
  if [ -f "$XG2G_DATA_DIR/$FILE" ]; then
    cp "$XG2G_DATA_DIR/$FILE" "$BACKUP_DIR/$FILE"
  fi
done
```

### Verify all known state

```bash
xg2g storage verify --all --mode quick
```

## Upgrade Path

1. Take an online backup of all primary durable state.
2. Install the new `xg2g` version.
3. Restart `xg2g`.
4. Run `xg2g storage verify --all --mode quick`.
