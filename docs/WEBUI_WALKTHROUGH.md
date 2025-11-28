# WebUI Walkthrough (v3.0.0)

This document provides a tour of the new **WebUI** introduced in xg2g v3.0.0. The WebUI offers a user-friendly interface to monitor and manage your xg2g instance without needing to use the command line.

## 🚀 Getting Started

The WebUI is enabled by default. Once your xg2g instance is running, open your browser and navigate to:

**[http://localhost:8080/ui/](http://localhost:8080/ui/)**

*(Replace `localhost` with your server's IP address if accessing remotely)*

---

## 🖥️ Dashboard

The **Dashboard** is your landing page. It gives you an immediate overview of the system's health.

### Key Indicators

- **Overall Health**: Green (OK) means everything is running smoothly. Yellow/Red indicates issues.
- **Receiver Status**: Shows if xg2g can communicate with your Enigma2 receiver.
- **EPG Status**: Indicates if EPG data is loaded and if any channels are missing guide data.
- **Uptime**: How long the daemon has been running.
- **Version**: The currently running xg2g version.

### Actions

- **Refresh Button**: Click "Refresh" in the top right to manually trigger a status update.

---

## 📺 Channels View

The **Channels** view allows you to browse your configured bouquets and verify channel mapping.

### Features

- **Bouquet Selection**: Use the sidebar to switch between your configured bouquets (e.g., "Favourites", "Movies").
- **Channel List**:
  - **#**: Channel number.
  - **Name**: Channel name as received from Enigma2.
  - **TVG ID**: The assigned ID for EPG mapping.
  - **EPG**: A status icon (✅/❌) showing if EPG data is available for this channel.

**Tip:** Use this view to quickly identify channels that are missing EPG data (marked with ❌).

---

## 📜 Logs View

The **Logs** view provides real-time insight into what xg2g is doing, useful for troubleshooting.

### Features

- **Recent Logs**: Displays the most recent log entries (warnings and errors are highlighted).
- **Auto-Refresh**: Click "Refresh" to load the latest logs.
- **Details**:
  - **Time**: Exact timestamp of the event.
  - **Level**: INFO, WARN, or ERROR.
  - **Component**: Which part of the system generated the log (e.g., `api`, `epg`, `proxy`).
  - **Message**: The actual log message.

---

## 🛠️ Development & Customization

The WebUI is built with **React** and **Vite**. The source code is located in the `webui/` directory.

### Building from Source

If you want to modify the WebUI:

1. **Navigate to the webui directory:**

    ```bash
    cd webui
    ```

2. **Install dependencies:**

    ```bash
    npm install
    ```

3. **Run in development mode:**

    ```bash
    npm run dev
    ```

    This will start a local dev server (usually at `http://localhost:5173`). Note that it will try to proxy API requests to `http://localhost:8080`.

4. **Build for production:**

    ```bash
    npm run build
    ```

    This generates the static assets in `webui/dist/`.

5. **Embed into Go binary:**
    Copy the contents of `webui/dist/` to `internal/api/ui/` and rebuild the Go application.

    ```bash
    rm -rf ../internal/api/ui/*
    cp -r dist/* ../internal/api/ui/
    cd ..
    go build ./cmd/daemon
    ```

---

## ❓ FAQ

**Q: Can I disable the WebUI?**
A: Currently, the WebUI is always enabled. A configuration option `XG2G_WEBUI_ENABLED` is planned for a future update.

**Q: Is the WebUI password protected?**
A: Not by default. It is designed for home network use. For public access, we recommend running xg2g behind a reverse proxy (like Caddy or Traefik) with authentication.
