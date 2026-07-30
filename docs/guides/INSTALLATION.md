# Install xg2g on Linux

This is the complete first-install guide for the supported 2026 server setup.
It starts with an empty Linux host and ends with a verified xg2g service, WebUI,
storage layout, HTTPS path, backups, and an operator command for later updates.

If xg2g is already installed, do not repeat the first-install wizard. Use
[`sudo xg2g-admin update --ref vX.Y.Z`](#updates-and-rollback) so the existing
configuration is preserved and a backup and automatic rollback are available.

## What the supported installation looks like

The standard installation is:

- a Linux `amd64` or `arm64` host with systemd;
- Docker Engine with the Compose v2 plugin;
- the pinned multi-architecture xg2g image, including FFmpeg;
- `xg2g.service`, supervised by systemd;
- an `xg2g(1)` manual page available through `man xg2g`;
- a backend exposed only as `127.0.0.1:8088`;
- protected configuration in `/etc/xg2g/xg2g.env`;
- durable state in `/var/lib/xg2g`;
- daily backup and runtime-verification timers; and
- HTTPS for every browser that is not running on the Linux host itself.

The installer does not partition, format, mount, or delete disks. It also does
not take over an existing reverse proxy. Managed Caddy is available only when
you explicitly select it.

## Choose the right path

| Goal | Path |
| :--- | :--- |
| Persistent home or production server | Follow this guide and use the official release archive. |
| Evaluate xg2g from a browser on the same Linux host | Use the [one-container evaluation](GETTING_STARTED.md#2-alternative-one-container-local-evaluation). |
| Develop or modify xg2g | Use the [developer setup](../dev/SETUP.md), not the server installer. |
| NAS/appliance without systemd | Use its native container manager and reproduce the persistence, secrets, HTTPS, health, and backup contracts manually. The guided host installer does not support this topology. |

Do not install from GitHub's green **Code → Download ZIP** button. A branch ZIP
is a mutable source snapshot and cannot prove which container image belongs to
it. The installer deliberately rejects it. Use the versioned Linux archive from
[GitHub Releases](https://github.com/ManuGH/xg2g/releases).

> **Release availability:** official Linux archives from `v3.9.0` onward are
> self-contained installation bundles and include
> `infra/systemd/setup-linux.sh`. Older archives predate the guided installer;
> do not combine installer files from `main` with an older image. The download
> block below validates the archive and stops with a clear error when a release
> predates the guided installer.

## 1. Prepare the Linux host

### Minimum requirements

- Linux with systemd, on `x86_64`/`amd64` or `aarch64`/`arm64`.
- Root access through `sudo`.
- A standard rootful Docker Engine and the `docker compose` v2 plugin.
- `curl`, `openssl`, `python3`, `tar`, `sha256sum`, `findmnt`, `ss`, and basic
  GNU/Linux administration tools.
- Network access from the Linux host to:
  - the Enigma2/OpenWebIF receiver;
  - `github.com` for the release bundle; and
  - `ghcr.io` for the pinned container image.
- Correct system time. Public certificate validation and ACME fail when the
  host clock is wrong.
- Enough CPU or a supported x86 GPU for the number of streams you expect.

The guided installer prints the appropriate prerequisite package command when
a required tool is missing. Common package commands are:

```bash
# Debian / Ubuntu
sudo apt-get update
sudo apt-get install -y ca-certificates curl openssl python3 tar coreutils util-linux iproute2

# Fedora / RHEL family
sudo dnf install -y ca-certificates curl openssl python3 tar coreutils util-linux iproute

# Arch family
sudo pacman -S --needed ca-certificates curl openssl python tar coreutils util-linux iproute2
```

Install Docker Engine and the Compose v2 plugin using the
[official Docker Engine instructions](https://docs.docker.com/engine/install/)
for your distribution. Then verify the actual runtime, not just the installed
packages:

```bash
systemctl is-system-running
sudo systemctl enable --now docker
sudo docker info
sudo docker compose version
timedatectl status
```

`docker compose version` must report Compose v2. The old standalone
`docker-compose` command is not the supported runtime.

### Verify the receiver first

Replace `RECEIVER_IP` with the Enigma2 receiver address:

```bash
curl -fsS http://RECEIVER_IP/api/about
```

If OpenWebIF uses HTTPS, use its HTTPS URL. If it requires authentication, the
wizard asks for the username and password. Fix routing, firewall, OpenWebIF, or
credentials before installing xg2g if this request cannot reach the receiver.

### Decide how browsers will connect

Choose before running the wizard:

| Browser location | Recommended setup choice |
| :--- | :--- |
| Browser runs on the Linux server | Local-only `http://localhost:8088`; useful for evaluation. |
| Phone, tablet, TV, or PC over LAN/VPN; HTTPS proxy already runs on this host | Private LAN/VPN + existing proxy. This is the default HTTPS choice. |
| Public internet; HTTPS proxy already runs on this host | Public + existing proxy. Apply the public exposure hardening. |
| No proxy; public DNS points to this host | Managed public Caddy, explicitly selected. Ports 80/443 must be free and reachable. |
| No proxy; private LAN/VPN only | Managed internal Caddy, explicitly selected. Clients must trust its private CA. |

Remote WebUI playback requires HTTPS. Plain HTTP to a LAN or VPN address is not
a supported browser setup. Local-only mode means a browser on the same Linux
host; it does not make `http://SERVER_IP:8088` safe or usable from another
device.

When using an existing proxy, configure it on the xg2g host to forward to
`http://127.0.0.1:8088` and preserve the normal forwarded headers. It may return
`502` until xg2g starts. The installer will later verify the complete HTTPS
path, HSTS, effective scheme, allowed origin, and trusted-proxy configuration.
See [Reverse proxy and HTTPS](../../infra/systemd/REVERSE_PROXY.md) for nginx,
Traefik, Caddy, tunnels, and advanced topologies.

Do not publish port 8088 as `0.0.0.0:8088`. The loopback binding prevents a
cleartext route around authentication and HTTPS.

### Decide where DVR scratch data belongs

The installer asks for the rewind window and simultaneous stream count, then
estimates capacity at a conservative 20 Mbit/s plus 20 percent headroom.

- **Simple/default:** keep durable data and HLS/DVR scratch on the system/data
  filesystem under `/var/lib/xg2g`.
- **Dedicated disk:** point HLS/DVR scratch at a directory on an
  already-mounted HDD, SSD, or NVMe filesystem.

An HDD is suitable for the mostly sequential DVR workload. SSD/NVMe provides
lower latency but is not required. Durable databases remain under
`/var/lib/xg2g`; transient HLS/DVR scratch is not part of backups.

For a dedicated disk, mount it persistently in `/etc/fstab` or with a systemd
mount unit before setup, then confirm it:

```bash
findmnt /path/to/mounted/disk
df -h /path/to/mounted/disk
```

The wizard enables a fail-closed mount check so a missing disk cannot silently
fill the root filesystem. See [Linux Storage Layout](../ops/STORAGE_LAYOUT.md)
for sizing and media tradeoffs.

### Decide how transcoding will run

- **Auto-detect** is the recommended first choice.
- **VAAPI** uses an Intel or AMD render device under `/dev/dri` on x86.
- **NVIDIA** requires a working NVIDIA driver and NVIDIA Container Toolkit.
- **CPU only** works everywhere, but multiple MPEG-2/HEVC-to-H.264 streams can
  require substantial CPU. On `arm64`, software encoding is the supported path.

## 2. Download and verify the release

For a release that carries the complete installation bundle, run the following
block on the Linux server. It detects the server architecture, resolves the
latest stable tag once, downloads that exact archive and `checksums.txt`,
verifies SHA-256, extracts the bundle, verifies the guided installer is
actually present, and starts it:

```bash
bash <<'INSTALL_SCRIPT'
set -euo pipefail

case "$(uname -m)" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

latest_url="$(
  curl --proto '=https' --tlsv1.2 -fsSL \
    -o /dev/null -w '%{url_effective}' \
    https://github.com/ManuGH/xg2g/releases/latest
)"
tag="${latest_url##*/}"
case "${tag}" in
  v[0-9]*) ;;
  *) echo "Could not resolve a release tag: ${tag}" >&2; exit 1 ;;
esac

version="${tag#v}"
archive="xg2g_${version}_linux_${arch}.tar.gz"
release_base="https://github.com/ManuGH/xg2g/releases/download/${tag}"
work_dir="$(mktemp -d)"
trap 'rm -rf "${work_dir}"' EXIT
cd "${work_dir}"

curl --proto '=https' --tlsv1.2 -fLO "${release_base}/${archive}"
curl --proto '=https' --tlsv1.2 -fLO "${release_base}/checksums.txt"
grep -F " ${archive}" checksums.txt > selected-checksum.txt
sha256sum --check selected-checksum.txt
tar -xzf "${archive}"

setup_script="$(find . -type f -path '*/infra/systemd/setup-linux.sh' -print -quit)"
if test -z "${setup_script}"; then
  echo "Release ${tag} predates the guided Linux installer." >&2
  echo "Use a newer release whose archive contains infra/systemd/setup-linux.sh." >&2
  exit 1
fi
bundle_root="${setup_script%/infra/systemd/setup-linux.sh}"
cd "${bundle_root}"
sudo ./infra/systemd/setup-linux.sh
INSTALL_SCRIPT
```

To install a specific version instead of the latest stable release, open its
[release page](https://github.com/ManuGH/xg2g/releases), download
`xg2g_<version>_linux_amd64.tar.gz` or
`xg2g_<version>_linux_arm64.tar.gz` and `checksums.txt`, verify the matching
line with `sha256sum --check`, extract it, confirm that
`infra/systemd/setup-linux.sh` exists, and run:

```bash
sudo ./infra/systemd/setup-linux.sh
```

The archive is self-contained. It does not clone a mutable branch during
installation, and the installed image ref is pinned to the archive version.

## 3. Answer the setup questions

The interactive wizard asks in this order:

1. **Receiver URL and optional OpenWebIF credentials.**
2. **Access mode:** same-host local, private LAN/VPN through HTTPS, or public
   HTTPS.
3. **HTTPS owner:** existing same-host proxy, managed public Caddy, or managed
   internal Caddy. Caddy is never selected implicitly.
4. **Browser origin:** the exact origin, such as
   `https://xg2g.example.net`, without a path.
5. **DVR window and simultaneous streams.**
6. **Storage:** shared filesystem or a dedicated, already-mounted path.
7. **Transcoding:** automatic, CPU, VAAPI, or NVIDIA.

During setup, xg2g:

- generates the API token and decision-signing secret;
- writes `/etc/xg2g/xg2g.env` as `root:root` with mode `0600`;
- creates data and HLS directories for container UID/GID `10001:10001`;
- installs the exact release bundle under `/srv/xg2g`;
- installs and starts the systemd/Compose service;
- enables daily durable-state backups and periodic runtime verification;
- pulls only the selected xg2g image and, when explicitly chosen, managed
  Caddy;
- checks storage, local readiness, and the configured HTTPS endpoint; and
- prints the WebUI URL and a new admin token.

Save the admin token when it is displayed. The setup run prints it only once.
Do not paste it into issue reports, screenshots, shell history, or chat.

If `/etc/xg2g/xg2g.env` already exists, the first-install wizard stops instead
of replacing secrets. Use `xg2g-admin update --ref vX.Y.Z` for a repair or
upgrade with the existing configuration; the lifecycle command invokes the
installed setup helper with `--keep-existing`.

## 4. Verify the installation

The installer already performs health and HTTPS checks. Run the operator
diagnostic once more for a clear final report:

```bash
sudo xg2g-admin doctor
sudo systemctl --no-pager --full status xg2g.service
curl -fsS http://127.0.0.1:8088/readyz
sudo /srv/xg2g/scripts/compose-xg2g.sh --storage-layout
systemctl list-timers xg2g-backup.timer xg2g-verifier.timer
```

All of these should succeed. `xg2g-admin doctor` verifies configuration
permissions, storage, Docker, systemd, local readiness, and the published HTTPS
endpoint.

If setup fails after the service was installed, inspect:

```bash
sudo journalctl -u xg2g.service -n 100 --no-pager
sudo /srv/xg2g/scripts/compose-xg2g.sh ps
sudo xg2g-admin doctor
```

Typical causes are an unreachable receiver, an unmounted dedicated DVR disk, a
missing Docker Compose v2 plugin, occupied ports 80/443 in managed-Caddy mode,
DNS/certificate errors, or an existing proxy that does not forward to
`127.0.0.1:8088`.

## 5. Open the WebUI

Use the exact URL printed by setup:

- local-only: `http://localhost:8088/ui/`, from a browser on the Linux host;
- existing or managed HTTPS: `https://YOUR_NAME/ui/`, from an allowed client.

Enter the generated admin token when the WebUI asks for it. For managed
internal Caddy, first install the printed CA certificate on every client:

```text
/var/lib/xg2g-caddy/data/caddy/pki/authorities/local/root.crt
```

Test a channel, seek backward inside the configured DVR window, then leave and
resume playback once. This proves receiver access, FFmpeg, storage, the browser
session, and the HTTPS path together.

## Routine operation

```bash
# Full installation and endpoint diagnosis
sudo xg2g-admin doctor

# Immediate verified backup
sudo xg2g-admin backup

# Service logs
sudo journalctl -u xg2g.service -f

# Restart after an operational issue
sudo systemctl restart xg2g.service
```

Daily backups are written under `/var/backups/xg2g`. They include durable
SQLite/JSON state and protected configuration, not transient HLS/DVR segments
or receiver recordings. Keep an additional copy on another filesystem or host;
a backup on the same disk is not disaster recovery.

## Updates and rollback

Choose an explicit release tag from
[GitHub Releases](https://github.com/ManuGH/xg2g/releases):

```bash
sudo xg2g-admin update --ref vX.Y.Z
```

The command creates a backup, installs the pinned ref, performs health checks,
and restores the previous ref automatically if startup fails. To deliberately
return to the recorded previous version:

```bash
sudo xg2g-admin rollback --yes
```

Never update a production host with an unpinned `latest` Compose edit or by
copying individual files into `/srv/xg2g`.

## Restore

List the available archives, then restore one explicitly:

```bash
sudo ls -lh /var/backups/xg2g
sudo xg2g-admin restore /var/backups/xg2g/ARCHIVE.tar.gz --yes
sudo xg2g-admin doctor
```

Restore validates the archive and creates a safety backup before replacing
durable state. See [Backup and Restore](../ops/BACKUP_RESTORE.md) for the full
state inventory and recovery procedure.

## Uninstall

The safe default removes the runtime and services but preserves configuration,
data, local backups, receiver recordings, and external DVR storage:

```bash
sudo xg2g-admin uninstall
```

Only use the destructive form when you intentionally want to remove xg2g's
local configuration, state, managed-Caddy state, and local backups:

```bash
sudo xg2g-admin uninstall --purge-data --yes
```

Even the purge form does not partition, format, or delete an external DVR path
or receiver recordings.

## Installed paths

| Path | Purpose |
| :--- | :--- |
| `/etc/xg2g/xg2g.env` | Protected runtime configuration and secrets |
| `/srv/xg2g` | Pinned deployment artifacts and lifecycle helpers |
| `/var/lib/xg2g` | Durable application state and default HLS root |
| `/var/backups/xg2g` | Local verified backup archives |
| `/usr/local/sbin/xg2g-admin` | Stable operator command |
| `/etc/systemd/system/xg2g.service` | Main systemd service |
| `/etc/systemd/system/xg2g-backup.*` | Daily backup service and timer |
| `/etc/systemd/system/xg2g-verifier.*` | Periodic runtime verification |
| `/etc/xg2g/Caddyfile` | Present only when managed Caddy was explicitly selected |
| `/var/lib/xg2g-caddy` | Present only for managed Caddy |

For the normative file modes, ownership, optional artifacts, and drift rules,
see the [Installation Contract](../ops/INSTALLATION_CONTRACT.md).
