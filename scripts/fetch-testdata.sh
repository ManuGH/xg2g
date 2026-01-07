#!/bin/bash
# Fetch test data for local development
# Test assets are NOT committed to keep repository lightweight

set -e

TESTDATA_DIR="testdata"
TESTDATA_URL="${TESTDATA_URL:-}" # Configurable via env var

echo "📦 Fetching test data for local development..."

# Create directories
mkdir -p "$TESTDATA_DIR"/{videos,segments,logs,scripts}

if [ -z "$TESTDATA_URL" ]; then
    echo "⚠️  TESTDATA_URL not set - skipping download"
    echo "📝 Test assets are gitignored to keep repo lightweight"
    echo "💡 To fetch from CDN: TESTDATA_URL=https://cdn.example.com/assets ./scripts/fetch-testdata.sh"
    echo ""
    echo "✅ testdata/ structure created"
    exit 0
fi

echo "🌐 Downloading from: $TESTDATA_URL"

# Download test files (examples - adjust URLs based on your CDN structure)
# Uncomment and modify as needed:
# curl -sL "$TESTDATA_URL/test_hevc.mp4" -o "$TESTDATA_DIR/videos/test_hevc.mp4" || echo "Warning: Download failed"
# curl -sL "$TESTDATA_URL/verify_seg.ts" -o "$TESTDATA_DIR/segments/verify_seg.ts" || echo "Warning: Download failed"

echo "✅ Test data fetch complete"
echo "📁 Files available in: $TESTDATA_DIR"
echo "📝 See testdata/README.md for usage"
