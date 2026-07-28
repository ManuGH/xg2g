# Linux Storage Layout

xg2g separates storage by lifecycle rather than assuming that every Linux host
has RAM, NVMe, SSD, and HDD tiers.

| Role | Default | Lifecycle | Backup |
| :--- | :--- | :--- | :--- |
| Persistent state | `XG2G_DATA=/var/lib/xg2g` | SQLite, configuration, generated metadata | Yes, according to the backup inventory |
| HLS/DVR scratch | `XG2G_HLS_ROOT=$XG2G_DATA/hls` | Live playlists, segments, and materialized recording HLS | No |
| Recordings | `/media/nfs-recordings` | Long-lived source recordings | Managed independently |
| RAM hot cache | Bounded in-process shadow store | Most recent finalized fMP4 segments | No |

The default needs only one writable filesystem. An external HLS path is an
optional optimization, not an installation requirement.

## Setup Inventory

The installed Compose helper reports the resolved paths, backing mounts,
filesystem types, media classes, and available capacity:

```bash
/srv/xg2g/scripts/compose-xg2g.sh --storage-layout
```

Run the fail-closed validation independently with:

```bash
/srv/xg2g/scripts/compose-xg2g.sh --storage-check
```

`infra/systemd/sync.sh --check` and `--apply` print the layout automatically on
the real host. The report is informational; the storage check is part of
`xg2g.service` startup.

Media classification is best-effort. `tmpfs` is reported as RAM, block-device
rotation data as SSD/NVMe or HDD, network filesystems as network, and mergerfs
as pooled storage. Virtualized, encrypted, and hardware-RAID devices may remain
`unknown`; path, mount, filesystem, size, and free-space data are still shown.

## Portable Profiles

### One filesystem

Do not set `XG2G_HLS_ROOT`. HLS falls back to
`/var/lib/xg2g/hls`. Keep enough free capacity for the selected DVR window.

### Dedicated SSD or NVMe scratch

Mount a host volume, create the scratch directory, and configure:

```bash
XG2G_HLS_ROOT=/var/lib/xg2g-hls
XG2G_HLS_REQUIRE_MOUNT=true
```

The Compose helper materializes a bind-mount overlay at runtime. The external
path is mounted at the identical absolute path inside the container, so the
application remains independent of the host's physical storage technology.

`XG2G_HLS_REQUIRE_MOUNT=true` fails startup when the configured HLS path
resolves to the same backing mount as `XG2G_DATA`. This protects against a
missing removable, LVM, ZFS, or hypervisor-provided scratch mount silently
falling back onto the system disk.

### HDD scratch

A dedicated HDD is acceptable for sequential DVR workloads. A shared mergerfs
or network pool is less predictable because segment creation, deletion, and
seek traffic can compete with recordings and backups. Keep the RAM shadow store
enabled when low-latency live playback matters.

## Capacity Planning

Approximate scratch consumption for one session:

```text
GiB = bitrate_Mbit_per_second × DVR_hours × 0.419
```

Add at least 20 percent headroom for audio, container overhead, temporary
segments, concurrent starts, and bitrate variation. Size or quota the dedicated
scratch volume so a full DVR window cannot exhaust the system filesystem.

Examples:

| Bitrate | DVR window | Approximate media | With 20% headroom |
| :--- | ---: | ---: | ---: |
| 8 Mbit/s | 2 h | 6.7 GiB | 8.1 GiB |
| 20 Mbit/s | 2 h | 16.8 GiB | 20.1 GiB |
| 20 Mbit/s | 4.5 h | 37.7 GiB | 45.3 GiB |

## Mount and Migration Order

1. Stop the affected xg2g environment.
2. Create and mount the dedicated scratch filesystem.
3. Create the configured `XG2G_HLS_ROOT` and verify it is writable.
4. Set `XG2G_HLS_ROOT` and, for dedicated mounts,
   `XG2G_HLS_REQUIRE_MOUNT=true`.
5. Run `--storage-layout` and `--storage-check`.
6. Recreate the container; a restart alone does not reload environment values.
7. Verify container health and confirm new HLS files appear on the intended
   mount.
8. Remove the old HLS directory only after the new runtime path is proven.

HLS artifacts are transient and are not migrated as durable state. Active live
sessions end during a storage-path migration and must be started again.
