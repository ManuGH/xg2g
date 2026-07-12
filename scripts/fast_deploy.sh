#!/usr/bin/env bash
set -e

echo "🚀 Syncing local changes to Proxmox host (10.10.55.2)..."
rsync -avz --exclude='.git' --exclude='node_modules' --exclude='bin' --exclude='build' --exclude='.idea' --exclude='.vscode' ./ root@10.10.55.2:/root/xg2g/

echo "🔨 Building on Proxmox..."
ssh root@10.10.55.2 'cd /root/xg2g && make build-with-ui'

echo "🛑 Stopping services in LXC..."
ssh root@10.10.55.2 'ssh 10.10.55.14 "docker stop xg2g-staging && systemctl stop xg2g"'

echo "📦 Pushing binary to LXC..."
ssh root@10.10.55.2 'pct push 110 /root/xg2g/bin/xg2g /srv/xg2g/xg2g-dev-binary && pct push 110 /root/xg2g/bin/xg2g /srv/xg2g-staging/xg2g-staging-binary'

echo "✅ Starting services in LXC..."
ssh root@10.10.55.2 'ssh 10.10.55.14 "systemctl start xg2g && docker start xg2g-staging"'

echo "🎉 Deployment complete!"
