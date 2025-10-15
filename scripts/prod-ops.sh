#!/usr/bin/env bash
# xg2g Production Rollback & Cleanup Script
set -euo pipefail

ACTION=${1:-help}

case "$ACTION" in
    rollback)
        echo "🔄 Rolling back xg2g production deployment..."
        
        # Stop current containers
        if [[ -f .env.prod ]]; then
            docker compose -f deploy/docker-compose.alpine.yml --env-file ./.env.prod down
        else
            docker compose -f deploy/docker-compose.alpine.yml down
        fi
        
        # Remove containers and images
        echo "🧹 Cleaning up containers and images..."
        docker container prune -f
        docker image rm ghcr.io/manugh/xg2g:alpine 2>/dev/null || true
        
        echo "✅ Rollback complete"
        ;;
        
    cleanup)
        echo "🧹 Cleaning up xg2g deployment artifacts..."
        
        # Stop services
        docker compose -f deploy/docker-compose.alpine.yml down 2>/dev/null || true
        
        # Remove data directory (ask for confirmation)
        if [[ -d ./data ]]; then
            read -p "❓ Remove data directory ./data? (y/N): " -n 1 -r
            echo
            if [[ $REPLY =~ ^[Yy]$ ]]; then
                rm -rf ./data
                echo "🗑️  Removed ./data directory"
            fi
        fi
        
        # Remove .env.prod (ask for confirmation)
        if [[ -f .env.prod ]]; then
            read -p "❓ Remove .env.prod file? (y/N): " -n 1 -r
            echo
            if [[ $REPLY =~ ^[Yy]$ ]]; then
                rm -f .env.prod
                echo "🗑️  Removed .env.prod"
            fi
        fi
        
        echo "✅ Cleanup complete"
        ;;
        
    logs)
        echo "📋 Showing xg2g service logs..."
        if [[ -f .env.prod ]]; then
            docker compose -f deploy/docker-compose.alpine.yml --env-file ./.env.prod logs -f --tail=100
        else
            docker compose -f deploy/docker-compose.alpine.yml logs -f --tail=100
        fi
        ;;
        
    status)
        echo "📊 xg2g Service Status Check"
        echo "============================"
        
        # Check if containers are running
        if docker compose -f deploy/docker-compose.alpine.yml ps | grep -q "Up"; then
            echo "✅ Containers: Running"
        else
            echo "❌ Containers: Stopped"
            exit 1
        fi
        
        # Check health endpoint
        if curl -sf http://localhost:8080/healthz >/dev/null 2>&1; then
            echo "✅ Health: OK"
        else
            echo "❌ Health: Failed"
        fi
        
        # Check readiness endpoint  
        if curl -sf http://localhost:8080/readyz >/dev/null 2>&1; then
            echo "✅ Ready: OK"
        else
            echo "⚠️  Ready: Not ready"
        fi
        
        # Check metrics endpoint
        if curl -sf http://localhost:9090/metrics >/dev/null 2>&1; then
            echo "✅ Metrics: OK"
        else
            echo "❌ Metrics: Failed"
        fi
        
        # Show current channel count
        CHANNELS=$(curl -sf http://localhost:8080/api/status 2>/dev/null | jq -r '.channels' 2>/dev/null || echo "unknown")
        echo "📺 Channels: $CHANNELS"
        ;;
        
    restart)
        echo "🔄 Restarting xg2g services..."
        if [[ -f .env.prod ]]; then
            docker compose -f deploy/docker-compose.alpine.yml --env-file ./.env.prod restart
        else
            docker compose -f deploy/docker-compose.alpine.yml restart
        fi
        echo "✅ Services restarted"
        ;;
        
    help|*)
        echo "🛠️  xg2g Production Management Commands"
        echo "======================================"
        echo "Usage: $0 <command>"
        echo ""
        echo "Commands:"
        echo "  rollback  - Stop services and remove containers/images"
        echo "  cleanup   - Full cleanup including data and config (interactive)"
        echo "  logs      - Show service logs (follow mode)"
        echo "  status    - Check service health and metrics"
        echo "  restart   - Restart all services"
        echo "  help      - Show this help message"
        echo ""
        echo "Examples:"
        echo "  $0 status          # Quick health check"
        echo "  $0 logs            # Watch logs in real-time"
        echo "  $0 rollback        # Emergency rollback"
        echo "  $0 cleanup         # Clean slate for fresh deployment"
        ;;
esac