# Proxmox LXC & Debian Trixie "BombShield" Guide

This guide addresses known critical issues when running xg2g (based on Debian Trixie) inside Proxmox LXC containers.

## 🚨 Critical Bombs & Fixes

### 1. LXC Docker Nesting Bug (CVE-2025-52881)

**Symptom:** Docker fails to start inside LXC with `permission denied`.
**Cause:** New AppArmor profile restrictions in recent Proxmox kernels.

**HOST FIX (`/etc/pve/lxc/<ID>.conf`):**
Add the following lines to your LXC configuration on the **Proxmox Host**:

```ini
lxc.apparmor.profile: unconfined
lxc.mount.entry: /dev/null sys/module/apparmor/parameters/enabled none bind 0 0
lxc.cgroup2.devices.allow: a
lxc.cap.drop:
```

*Reboot the container after applying.*

---

### 2. Debian Trixie DNS Resolution Bug

**Symptom:** Containers cannot resolve domains ("Temporary failure resolving").
**Cause:** `systemd-resolved` changes in Trixie conflict with some Docker network defaults.

**FIX (Applied in `docker-compose.yml`):**
We explicitly force DNS servers for the container:

```yaml
dns:
  - 8.8.8.8
  - 1.1.1.1
```

---

### 3. Docker Socket Permissions

**Symptom:** `permission denied` when running `docker compose` or interacting with the daemon.
**Cause:** Trixie updates may reset `docker.sock` permissions to 660 (root:docker) and your user might not be in the group correctly.

**FIX (Inside LXC):**

```bash
# 1. Add current user to docker group
sudo usermod -aG docker $USER

# 2. Fix socket permissions (if strictly needed)
sudo chmod 666 /var/run/docker.sock
```

---

### 4. OpenWebIF Network Timeout (MTU)

**Symptom:** Streams drop or timeout abruptly.
**Cause:** MTU mismatch between Docker overlay network (1500) and some host interfaces.

**FIX:** Use `network_mode: host` in `docker-compose.yml` (recommended for Home Lab):

```yaml
# ports: ... (comment out ports)
network_mode: host
```

---

### 5. Enigma2 H.264 Header Repair

**Symptom:** Plex/Jellyfin shows "Playback Error" for live streams.
**Cause:** Enigma2 streams lack PPS/SPS headers required by many players.

**FIX:** Ensure this is enabled in `.env` (Default: true):

```env
XG2G_H264_STREAM_REPAIR=true
```
