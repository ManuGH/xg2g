# HLS Root Path Migration

The HLS segment storage path has been standardized. This guide explains how the automatic migration works and how to configure your environment.

## What Changed?

* **Old Default**: `data/v3-hls`
* **New Default**: `data/hls`
* **Primary Env**: `XG2G_HLS_ROOT`
* **Deprecated Env**: `XG2G_V3_HLS_ROOT` (Removal floor: **v3.4**)

| Environment      | Default DataDir | Resolved HLS Root |
| :---             | :---            | :---              |
| **Docker Compose** | `/data`         | `/data/hls`       |
| **Systemd**       | `/var/lib/xg2g` | `/var/lib/xg2g/hls`|
| **Local / Dev**   | `./data`        | `./data/hls`      |

## Automatic Migration

When the server starts without any explicit HLS root configuration:

1. Checks if `data/hls` exists. If so, it is used.
2. Checks if `data/v3-hls` exists.
    * If found, the server attempts to **rename** it to `data/hls`.
    * A marker file `.xg2g_migrated_from_v3` is placed in the new directory.
    * If the rename fails (e.g., permissions), the server safely falls back to using the legacy path.

**Symlink Safety**: If your `data/v3-hls` is a symlink, **no migration is attempted**. The server will issue a warning and continue using the symlinked path.

## How to Override

You can opt-out of these defaults by setting an explicit path:

```bash
# Docker / Shell
XG2G_HLS_ROOT=/mnt/storage/custom-hls
```

If you set this variable, **no automatic migration** will occur, even if legacy folders exist.

## Rollback / Compatibility

If you need to force the old path for compatibility:

```bash
XG2G_V3_HLS_ROOT=/path/to/old/v3-hls
```

Using this variable will trigger a deprecated warning log but restores the v3.1 behavior.

## Troubleshooting

* **"Both XG2G_HLS_ROOT and XG2G_V3_HLS_ROOT are set"**: The server will use the new variable. Remove the old one to clear the warning.
* **"Legacy HLS root is a symlink"**: Automatic migration skipped. Manually update your mount points or configuration if you wish to use the new path structure.
* **Symlink Support**: If `/data/hls` is a symlink, it is fully supported; xg2g will resolve it to a directory.
