#!/usr/bin/env bash
# xg2g Auto-Refresh Cron Script
# Usage: Add to crontab for automatic refresh
#        */15 * * * * /path/to/xg2g-auto-refresh.sh
set -euo pipefail

# Configuration
XG2G_HOST=${XG2G_HOST:-localhost:8080}
XG2G_TOKEN_FILE=${XG2G_TOKEN_FILE:-/path/to/.env.prod}
LOG_FILE=${LOG_FILE:-/var/log/xg2g-refresh.log}

# Extract token from .env.prod file
if [[ -f "$XG2G_TOKEN_FILE" ]]; then
    API_TOKEN=$(grep ^XG2G_API_TOKEN "$XG2G_TOKEN_FILE" | cut -d= -f2)
else
    echo "$(date): ERROR - Token file not found: $XG2G_TOKEN_FILE" >> "$LOG_FILE"
    exit 1
fi

# Refresh function
refresh_xg2g() {
    local response
    response=$(curl -sf -X POST "http://$XG2G_HOST/api/refresh" \
        -H "X-API-Token: $API_TOKEN" 2>&1)
    
    if [[ $? -eq 0 ]]; then
        local channels=$(echo "$response" | jq -r '.channels // 0' 2>/dev/null || echo "unknown")
        echo "$(date): SUCCESS - Refresh completed, channels: $channels"
    else
        echo "$(date): ERROR - Refresh failed: $response"
        return 1
    fi
}

# Main execution
{
    echo "$(date): Starting xg2g auto-refresh..."
    
    # Health check first
    if ! curl -sf "http://$XG2G_HOST/healthz" >/dev/null; then
        echo "$(date): ERROR - Health check failed, xg2g service unreachable"
        exit 1
    fi
    
    # Perform refresh
    refresh_xg2g
    
} >> "$LOG_FILE" 2>&1