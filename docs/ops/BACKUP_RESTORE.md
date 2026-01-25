# SQLite Operations: Backup and Restore

As of xg2g v3.0.0, SQLite is the **Single Durable Truth** for all stateful components. Legacy BoltDB and JSON backends have been removed.

## Durable Database Inventory

The following databases persist xg2g state (paths relative to `XG2G_DATA_DIR`):

| Component | Database File | Description |
| :--- | :--- | :--- |
| Sessions | `sessions.sqlite` | Active and historical session state |
| Resumes | `resume.sqlite` | Pipeline resumption metadata |
| Capabilities | `capabilities.sqlite` | Hardware/Transcode scan results |

## Backup Strategy

Always take a backup before upgrading xg2g versions.

### 1. Online Backup (Recommended)

This is the fastest and safest method to take a consistent snapshot while the daemon is running. It uses SQLite's internal backup mechanism.

```bash
# Example: Online backup of sessions database
sqlite3 /var/lib/xg2g/sessions.sqlite ".backup '/var/backups/xg2g/sessions-$(date +%F).sqlite'"
```

### 2. Online Backup (Compacted)

Produces a smaller, defragmented backup file using `VACUUM INTO`.
> [!IMPORTANT]
> This requires free disk space equal to the size of the database. If it fails mid-way, the operator must manually delete the partial backup file.

```bash
sqlite3 /var/lib/xg2g/sessions.sqlite "VACUUM INTO '/var/backups/xg2g/sessions-compact.sqlite'"
```

### 3. Cold Backup (Stop Service First)

If the daemon is stopped, follow this ritual to ensure WAL files are safely merged:

1. Stop the `xg2g` daemon.
2. **Mandatory**: Merge WAL logs before copying.

   ```bash
   sqlite3 /var/lib/xg2g/sessions.sqlite ".backup '/var/backups/xg2g/sessions-cold.sqlite'"
   ```

3. Alternatively, copy the main `.sqlite` file **along with** its `-wal` and `-shm` side-files.

## Restore Strategy

1. Stop the `xg2g` daemon.
2. Replace the target `.sqlite` file with the backup (and delete any existing `-wal`/`-shm` files to avoid corruption).
3. Start the `xg2g` daemon. The built-in auto-migration will handle any structural updates.

## Integrity Verification

Operators should verify health regularly or after an unclean shutdown.

### Built-in Tool

```bash
# Verify all databases in $XG2G_DATA_DIR (routine)
xg2g storage verify --all --mode quick

# Deep diagnosis of a specific file
xg2g storage verify --path "$XG2G_DATA_DIR/sessions.sqlite" --mode full
```

### Manual Verification

```bash
# Fast check
sqlite3 "$XG2G_DATA_DIR/sessions.sqlite" "PRAGMA quick_check;"

# Deep check
sqlite3 "$XG2G_DATA_DIR/sessions.sqlite" "PRAGMA integrity_check;"
```

If the output is `ok`, the database is sound.

## Automation Examples

### Iterate and Backup All Databases

Operators can use this loop to snapshot the entire state:

```bash
DBS=("sessions.sqlite" "resume.sqlite" "capabilities.sqlite")
BACKUP_DIR="/var/backups/xg2g/$(date +%F)"
mkdir -p "$BACKUP_DIR"

for DB in "${DBS[@]}"; do
    sqlite3 "$XG2G_DATA_DIR/$DB" ".backup '$BACKUP_DIR/$DB'"
done
```

### Iterate and Verify All Databases

```bash
# Using the built-in batch mode (requires $XG2G_DATA_DIR)
xg2g storage verify --all --mode quick

# Or using a manual loop
for DB in "$XG2G_DATA_DIR"/*.sqlite; do
    xg2g storage verify --path "$DB" --mode quick
done
```

## Upgrade Path

1. **Safety Ritual**: Take an online backup of all `.sqlite` files.
2. Install new xg2g version.
3. Restart `xg2g`. Auto-migrations run within transactions.
4. Verify success: run `xg2g storage verify` on all databases.
