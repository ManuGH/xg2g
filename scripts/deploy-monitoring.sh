#!/bin/bash
set -euo pipefail

# xg2g Production Deployment Script
# Usage: ./deploy-monitoring.sh [environment]

ENVIRONMENT=${1:-staging}
COMPOSE_FILE="docker-compose.monitoring.yml"

echo "🚀 Deploying xg2g with monitoring stack to: $ENVIRONMENT"

# Check prerequisites
if ! command -v docker &> /dev/null; then
    echo "❌ Docker not found. Please install Docker first."
    exit 1
fi

if ! command -v docker-compose &> /dev/null && ! docker compose version &> /dev/null; then
    echo "❌ Docker Compose not found. Please install Docker Compose first."
    exit 1
fi

# Create environment file if it doesn't exist
ENV_FILE=".env.${ENVIRONMENT}"
if [[ ! -f "$ENV_FILE" ]]; then
    echo "📝 Creating environment file: $ENV_FILE"
    cat > "$ENV_FILE" << EOF
# xg2g Configuration for ${ENVIRONMENT}
XG2G_OWI_BASE=http://receiver.local
XG2G_BOUQUET=Favourites
XG2G_PICON_BASE=
GRAFANA_ADMIN_PASSWORD=admin
EOF
    echo "⚠️  Please edit $ENV_FILE with your actual configuration!"
fi

# Create monitoring directories if they don't exist
echo "📁 Setting up monitoring directories..."
mkdir -p monitoring/grafana/{provisioning/{datasources,dashboards},dashboards}
mkdir -p data

# Validate configuration files
echo "🔍 Validating configuration files..."
if [[ ! -f "monitoring/prometheus.yml" ]]; then
    echo "❌ monitoring/prometheus.yml not found"
    exit 1
fi

if [[ ! -f "monitoring/alert.rules.yml" ]]; then
    echo "❌ monitoring/alert.rules.yml not found"
    exit 1
fi

if [[ ! -f "monitoring/grafana/dashboards/xg2g-dashboard.json" ]]; then
    echo "❌ Grafana dashboard not found"
    exit 1
fi

# Build and start services
echo "🔨 Building xg2g application..."
docker build -t xg2g:latest .

echo "🚀 Starting monitoring stack..."
docker-compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" up -d

# Wait for services to be ready
echo "⏳ Waiting for services to start..."
sleep 10

# Health checks
echo "🏥 Performing health checks..."

# Check xg2g app
if curl -sf http://localhost:8080/healthz > /dev/null; then
    echo "✅ xg2g app is healthy"
else
    echo "❌ xg2g app health check failed"
fi

# Check Prometheus
if curl -sf http://localhost:9091/-/healthy > /dev/null; then
    echo "✅ Prometheus is healthy"
else
    echo "❌ Prometheus health check failed"
fi

# Check Grafana
if curl -sf http://localhost:3000/api/health > /dev/null; then
    echo "✅ Grafana is healthy"
else
    echo "❌ Grafana health check failed"
fi

# Check AlertManager
if curl -sf http://localhost:9093/-/healthy > /dev/null; then
    echo "✅ AlertManager is healthy"
else
    echo "❌ AlertManager health check failed"
fi

echo ""
echo "🎉 Deployment completed!"
echo ""
echo "📊 Access URLs:"
echo "  xg2g Application: http://localhost:8080"
echo "  xg2g Metrics:     http://localhost:9090/metrics"
echo "  Grafana:          http://localhost:3000 (admin/admin)"
echo "  Prometheus:       http://localhost:9091"
echo "  AlertManager:     http://localhost:9093"
echo ""
echo "🔧 Next steps:"
echo "  1. Configure OpenWebIF base URL in $ENV_FILE"
echo "  2. Access Grafana dashboard: http://localhost:3000/d/xg2g-main"
echo "  3. Setup AlertManager notifications in monitoring/alertmanager.yml"
echo "  4. Run security tests: ./scripts/security-test.sh"
echo ""
