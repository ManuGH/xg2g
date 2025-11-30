# xg2g WebUI Guide (v3.2)

The xg2g WebUI provides a "Zero-SSH" management interface for your gateway. It allows you to monitor system health, view logs, and manage playlist/EPG files directly from your browser.

## Accessing the WebUI

By default, the WebUI is available at:

- **URL**: `http://<your-xg2g-ip>:8080/ui/`
- **Port**: 8080 (or configured `XG2G_API_PORT`)

> [!NOTE]
> Ensure `XG2G_WEBUI_ENABLED=true` is set in your configuration (default is true).

## Features

### 1. Dashboard

The Dashboard provides an at-a-glance view of your system's health.

- **System Status**: Overall health, uptime, and version.
- **Receiver Status**: Connectivity to your Enigma2 receiver.
- **EPG Status**: Coverage information and missing channel counts.
- **Recent Logs**: A live view of the last 5 system logs (errors, warnings, info) to help troubleshoot issues immediately.

### 2. Channels

Browse your available channels and bouquets.

- Filter by bouquet.
- Search for specific channels.
- View EPG mapping status for each channel.

### 3. Files (New in v3.2)

Manage your generated playlist and guide files without command-line access.

- **M3U Playlist**:
  - View file size and last modification time.
  - **Download**: Click to download the `playlist.m3u` file for use in players like VLC or TiviMate.
  - **Regenerate**: Trigger a manual refresh to update the playlist from your receiver.
- **XMLTV Guide**:
  - View file size and status.
  - **Download**: Click to download the `xmltv.xml` file.

## API Endpoints

The WebUI is built on top of the xg2g API. You can use these endpoints directly for automation:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/health` | GET | System health status |
| `/api/logs/recent` | GET | Get recent log entries (JSON) |
| `/api/files/status` | GET | Status of M3U/XMLTV files |
| `/api/m3u/download` | GET | Download generated M3U playlist |
| `/api/xmltv/download` | GET | Download generated XMLTV guide |
| `/api/m3u/regenerate` | POST | Trigger playlist/EPG refresh |

## Troubleshooting

If the WebUI is not loading:

1. Check if the xg2g container/daemon is running.
2. Verify port 8080 is exposed and accessible.
3. Check logs for any startup errors: `docker logs xg2g`.
