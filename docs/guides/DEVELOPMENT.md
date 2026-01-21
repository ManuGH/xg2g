# Development Policy: Safe Process Management

To ensure development stability and prevent accidental session lockouts (SSH disconnects), all process termination within this repository must follow these safety guidelines.

## 🛡️ SSH Stability Rules

1. **Avoid `pkill` without filters**: Never use broad commands like `pkill -u $USER`. This will terminate your SSH agent and session.
2. **Targeted Termination**: Always use the `-f` flag with a specific process name or use PID tracking.
   - **Correct**: `pkill -f xg2g` (targets only the xg2g binary)
   - **Correct**: `pkill -f run_dev.sh` (targets only the dev loop)
3. **Control Plane Isolation**: When testing shutdowns, use the built-in diagnostic tools or container signals rather than host-wide process signals.

## 🏗️ Execution Contexts: Dev vs. System

It is critical to distinguish between development and production.

### `run_dev.sh` (Development Loop)

- **Purpose**: Rapid iteration and local debugging.
- **Behavior**: Infinite loop; auto-rebuilds and restarts on crash.
- **Logs**: Captured in `logs/dev.log`.
- **Usage**: Internal dev only; not valid for audit verification.

### System / Production (Hardened Container)

- **Standard**: **OCI Image is Source of Truth for Runtime.**
- **Supervisor**: **systemd** (manages Docker/Podman lifecycle).
- **Behavior**: Single execution lifecycle; formal hardening (v3.1.4).
- **Usage**: Mandatory for releases, sign-offs, and verification.

## 🛠️ Recommended Shutdown Pattern

### Local Development

To stop the application and its dev-loop safely without killing SSH:

```bash
./scripts/safe-shutdown.sh
```

### Production / System (Docker Compose)

Use the standard lifecycle commands:

```bash
docker compose down
# OR via systemd if installed
systemctl stop xg2g
```

### Local Compose Overrides

For local development with Compose, apply the dev override:

```bash
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d
```

*Note: This script targets only `xg2g` and `run_dev.sh` processes.*

### Containerized Testing

Use `docker stop` to leverage graceful SIGTERM propagation without affecting the host environment:

```bash
docker stop $(docker ps -q --filter name=xg2g)
```

## 📜 Continuous Verification

Maintainers and AI Agents must verify that verification scripts (e.g., `test-shutdown.sh`) do not execute any commands that could compromise the interactive shell or connection.
