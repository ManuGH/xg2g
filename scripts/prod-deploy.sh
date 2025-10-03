#!/usr/bin/env bash
# xg2g Production Quick Start Script
set -euo pipefail

echo "🚀 xg2g Production Deployment Quick Start"
echo "========================================="

# Check if .env.prod exists
if [[ ! -f .env.prod ]]; then
    echo "❌ .env.prod not found. Creating from template..."
    cp .env.prod.template .env.prod
    echo "📝 Please edit .env.prod with your actual values:"
    echo "   - XG2G_OWI_BASE (your receiver URL)"
    echo "   - XG2G_BOUQUET (your bouquet name)"  
    echo "   - XG2G_API_TOKEN (secure random token)"
    echo ""
    echo "💡 Generate token: openssl rand -hex 32"
    exit 1
fi

echo "✅ Found .env.prod configuration"

# Pull latest images
echo "🔄 Pulling Docker images..."
docker compose -f deploy/docker-compose.alpine.yml --env-file ./.env.prod pull

# Start services
echo "🚀 Starting xg2g services..."
docker compose -f deploy/docker-compose.alpine.yml --env-file ./.env.prod up -d

# Wait for startup
echo "⏳ Waiting for service startup..."
sleep 3

# Health checks
echo "🔍 Running health checks..."
if curl -sf http://localhost:8080/healthz >/dev/null; then
    echo "✅ Health check passed"
else
    echo "❌ Health check failed"
    exit 1
fi

# Get API token from .env.prod
API_TOKEN=$(grep ^XG2G_API_TOKEN .env.prod | cut -d= -f2)

# Trigger refresh
echo "🔄 Triggering initial refresh..."
if curl -sf -X POST http://localhost:8080/api/refresh -H "X-API-Token: $API_TOKEN" >/dev/null; then
    echo "✅ Refresh triggered successfully"
else
    echo "⚠️  Refresh failed - check logs and configuration"
fi

# Check readiness
echo "📊 Checking service readiness..."
if curl -sf http://localhost:8080/readyz >/dev/null; then
    echo "✅ Service is ready"
else
    echo "⚠️  Service not ready yet - may need more time"
fi

# Final status
echo ""
echo "🎉 xg2g Production Deployment Complete!"
echo "======================================"
echo "📊 Status:     http://localhost:8080/api/status"
echo "📈 Metrics:    http://localhost:9090/metrics"  
echo "🔍 Logs:       docker compose -f deploy/docker-compose.alpine.yml logs -f"
echo "🛑 Stop:       docker compose -f deploy/docker-compose.alpine.yml down"