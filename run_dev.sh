#!/usr/bin/env bash
set -euo pipefail

# Standardized Development Wrapper
# Uses .env and delegates to 'make dev' for consistent builds.

echo "🚀 Starting xg2g via 'make dev'..."
exec make dev
