# Installation Guide: NAS Systems (Unraid & Synology)

`xg2g` runs perfectly on modern NAS systems via Docker. This guide covers the setup for Unraid and Synology.

> **Note**: For these platforms, we strongly recommend using the **Host Network** mode to ensure HDHomeRun discovery (SSDP) works correctly with clients like Plex or Channels DVR.

## 1. Unraid

### Method A: Docker Compose (Preferred)

If you have the "Docker Compose Manager" plugin installed:

1. Create a new stack named `xg2g`.
2. Paste the standard [docker-compose.yml](../../docker-compose.yml) contents.
3. The docker-compose.yml already references the correct image (`ghcr.io/manugh/xg2g:latest`).
4. Click **Up**.

### Method B: "Add Container" (Manual)

1. Go to **Docker** > **Add Container**.
2. **Name**: `xg2g`
3. **Repository**: `ghcr.io/manugh/xg2g:latest`
4. **Network Type**: `Host`
5. **Paths (Volume Mappings)**:
   - **Click "Add another Path, Port, Variable..."**
   - **Config Type**: Path
   - **Name**: Data
   - **Container Path**: `/data`
   - **Host Path**: `/mnt/user/appdata/xg2g/`
6. **Variables**:
   - Add a variable for your receiver URL:
     - **Name**: Receiver URL
     - **Key**: `XG2G_OWI_BASE`
     - **Value**: `http://192.168.x.x` (Your Enigma2/Dreambox IP)
   - (Optional) Add `XG2G_API_TOKEN` if you want to secure the UI.
7. Click **Apply**.

---

## 2. Synology (Container Manager)

1. Open **Container Manager** (formerly Docker).
2. Go to **Registry** and search for `xg2g`.
   - *Note: If not found, add `ghcr.io` to your Registry settings or download the image manually via SSH `docker pull ghcr.io/manugh/xg2g:latest`.*
3. Select the image and click **Run**.
4. **General Settings**:
   - **Container Name**: `xg2g`
   - **Enable auto-restart**: Checked
5. **Advanced Settings**:
   - **Network**: Select "Use the same network as Docker Host" (Crucial for discovery).
6. **Volume Settings**:
   - Add Folder: Create/Select `docker/xg2g`
   - Mount path: `/data`
7. **Environment Variables**:
   - Add `XG2G_OWI_BASE` = `http://192.168.x.x`
   - Add `XG2G_LOG_LEVEL` = `info`
8. Click **Next** until **Done**.

## 3. Verification

Once running, access the dashboard at:
`http://<NAS-IP>:8088/ui/`

If configured correctly, your playlist is available at:
`http://<NAS-IP>:8088/playlist.m3u`
