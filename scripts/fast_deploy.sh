#!/usr/bin/env bash
set -euo pipefail

PROXMOX_HOST="root@10.10.55.2"
PROXMOX_DIR="/root/xg2g"
LXC_ID="110"

echo "🚀 Starting fast-track deployment to Proxmox / LXC 110..."

echo "📦 1. Rsyncing local code to Proxmox build server ($PROXMOX_HOST:$PROXMOX_DIR)..."
# Fast rsync excluding heavy/unnecessary directories
rsync -avz --delete \
  --exclude='.git' \
  --exclude='node_modules' \
  --exclude='frontend/webui/node_modules' \
  --exclude='.venv' \
  --exclude='bin' \
  --exclude='artifacts' \
  ./ "$PROXMOX_HOST:$PROXMOX_DIR/"

echo "🔨 2. Compiling on Proxmox ($PROXMOX_HOST)..."
ssh "$PROXMOX_HOST" "cd $PROXMOX_DIR && make build-with-ui"

echo "🛑 3. Stopping services on LXC 110..."
ssh "$PROXMOX_HOST" "pct exec $LXC_ID -- systemctl stop xg2g || true"
ssh "$PROXMOX_HOST" "pct exec $LXC_ID -- sh -c 'cd /srv/xg2g-staging && docker compose stop || true'"

echo "🚚 4. Pushing binary to Staging/Prod LXC 110..."
# Copy the compiled binary from the Proxmox host into the LXC container
ssh "$PROXMOX_HOST" "pct push $LXC_ID $PROXMOX_DIR/bin/xg2g /srv/xg2g/xg2g"
ssh "$PROXMOX_HOST" "pct push $LXC_ID $PROXMOX_DIR/bin/xg2g /srv/xg2g-staging/xg2g || true"

# Ensure binary is executable inside the LXC
ssh "$PROXMOX_HOST" "pct exec $LXC_ID -- chmod +x /srv/xg2g/xg2g"
ssh "$PROXMOX_HOST" "pct exec $LXC_ID -- chmod +x /srv/xg2g-staging/xg2g || true"

echo "✅ 5. Starting services on LXC 110..."
ssh "$PROXMOX_HOST" "pct exec $LXC_ID -- systemctl start xg2g"
ssh "$PROXMOX_HOST" "pct exec $LXC_ID -- sh -c 'cd /srv/xg2g-staging && docker compose start || true'"

echo "🎉 Deployment complete!"
