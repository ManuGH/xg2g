#!/usr/bin/env bash
# xg2g XMLTV Smoke Test - Verify M3U + XMLTV generation
set -euo pipefail

echo "🧪 xg2g XMLTV Smoke Test"
echo "========================"

# Check if .env.prod exists
if [[ ! -f .env.prod ]]; then
    echo "❌ .env.prod not found. Run smoke test on existing deployment:"
    echo "   XG2G_XMLTV=xmltv.xml ./scripts/xmltv-smoke-test.sh"
    exit 1
fi

# Get API token from .env.prod
API_TOKEN=$(grep ^XG2G_API_TOKEN .env.prod | cut -d= -f2)
DATA_DIR=$(grep ^XG2G_DATA .env.prod | cut -d= -f2 2>/dev/null || echo "./data")
XMLTV_PATH=$(grep ^XG2G_XMLTV .env.prod | cut -d= -f2 2>/dev/null || echo "")

if [[ -z "$XMLTV_PATH" ]]; then
    echo "⚠️  XG2G_XMLTV not set in .env.prod - XMLTV generation disabled"
    echo "💡 To enable: Add 'XG2G_XMLTV=xmltv.xml' to .env.prod and restart"
    exit 1
fi

echo "✅ Configuration check passed"
echo "   📁 Data Dir: $DATA_DIR"
echo "   📄 XMLTV: $XMLTV_PATH"

# Check service health first
echo ""
echo "🔍 Checking service health..."
if ! curl -sf http://localhost:8080/healthz >/dev/null; then
    echo "❌ Service health check failed - is xg2g running?"
    exit 1
fi

echo "✅ Service is healthy"

# Clear existing files to ensure fresh generation
echo ""
echo "🧹 Clearing existing artifacts..."
rm -f "$DATA_DIR/playlist.m3u" "$DATA_DIR/$XMLTV_PATH"

# Trigger refresh
echo ""
echo "🔄 Triggering refresh with XMLTV generation..."
REFRESH_RESPONSE=$(curl -sf -X POST http://localhost:8080/api/refresh \
    -H "X-API-Token: $API_TOKEN" 2>/dev/null || echo "FAILED")

if [[ "$REFRESH_RESPONSE" == "FAILED" ]]; then
    echo "❌ Refresh failed - check logs:"
    echo "   docker compose -f deploy/docker-compose.alpine.yml logs xg2g --tail=20"
    exit 1
fi

echo "✅ Refresh triggered successfully"

# Wait a moment for file generation
echo ""
echo "⏳ Waiting for file generation..."
sleep 2

# Check M3U playlist
echo ""
echo "📺 Checking M3U playlist..."
M3U_PATH="$DATA_DIR/playlist.m3u"
if [[ ! -f "$M3U_PATH" ]]; then
    echo "❌ M3U playlist not found at: $M3U_PATH"
    exit 1
fi

M3U_LINES=$(wc -l < "$M3U_PATH")
M3U_CHANNELS=$(grep -c "^#EXTINF" "$M3U_PATH" || echo 0)
echo "✅ M3U playlist generated: $M3U_LINES lines, $M3U_CHANNELS channels"

# Check XMLTV file
echo ""
echo "📄 Checking XMLTV file..."
XMLTV_FULL_PATH="$DATA_DIR/$XMLTV_PATH"
if [[ ! -f "$XMLTV_FULL_PATH" ]]; then
    echo "❌ XMLTV file not found at: $XMLTV_FULL_PATH"
    echo "🔍 Check logs for XMLTV generation errors:"
    echo "   docker compose -f deploy/docker-compose.alpine.yml logs xg2g | grep xmltv"
    exit 1
fi

XMLTV_SIZE=$(stat -f%z "$XMLTV_FULL_PATH" 2>/dev/null || stat -c%s "$XMLTV_FULL_PATH" 2>/dev/null || echo 0)
XMLTV_CHANNELS=$(grep -c "<channel " "$XMLTV_FULL_PATH" || echo 0)
echo "✅ XMLTV file generated: $XMLTV_SIZE bytes, $XMLTV_CHANNELS channels"

# Validate XML structure
echo ""
echo "🔍 Validating XMLTV structure..."
if command -v xmllint >/dev/null 2>&1; then
    if xmllint --noout "$XMLTV_FULL_PATH" 2>/dev/null; then
        echo "✅ XMLTV XML structure is valid"
    else
        echo "⚠️  XMLTV XML structure may have issues"
    fi
else
    echo "💡 xmllint not found - skipping XML validation"
fi

# Check if channel counts match
echo ""
echo "🔄 Comparing channel counts..."
if [[ "$M3U_CHANNELS" -eq "$XMLTV_CHANNELS" ]]; then
    echo "✅ Channel counts match: M3U=$M3U_CHANNELS, XMLTV=$XMLTV_CHANNELS"
else
    echo "⚠️  Channel count mismatch: M3U=$M3U_CHANNELS, XMLTV=$XMLTV_CHANNELS"
fi

# Show sample content
echo ""
echo "📋 Sample M3U content:"
head -10 "$M3U_PATH" | sed 's/^/   /'

echo ""
echo "📋 Sample XMLTV content:"
head -15 "$XMLTV_FULL_PATH" | sed 's/^/   /'

# Final status
echo ""
echo "🎉 XMLTV Smoke Test Complete!"
echo "============================="
echo "📺 M3U: $M3U_CHANNELS channels → $M3U_PATH"
echo "📄 XMLTV: $XMLTV_CHANNELS channels → $XMLTV_FULL_PATH"
echo "🌐 Files served at:"
echo "   http://localhost:8080/files/playlist.m3u"
echo "   http://localhost:8080/files/$XMLTV_PATH"

# Check metrics
echo ""
echo "📊 XMLTV Metrics:"
curl -sf http://localhost:9090/metrics 2>/dev/null | grep -E 'xg2g_xmltv|xg2g_channels' | sed 's/^/   /' || echo "   ⚠️ Metrics not available"
