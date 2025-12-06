#!/usr/bin/env bash
# Auto-detect BASE_VARIANT based on host OS
# Usage: source detect-base-variant.sh

set -e

if [ -f /etc/os-release ]; then
    . /etc/os-release

    case "$ID" in
        alpine)
            export BASE_VARIANT="alpine"
            ;;
        debian)
            case "$VERSION_CODENAME" in
                trixie)
                    export BASE_VARIANT="trixie"
                    ;;
                bookworm)
                    export BASE_VARIANT="bookworm"
                    ;;
                *)
                    export BASE_VARIANT="bookworm"  # Default for unknown Debian versions
                    ;;
            esac
            ;;
        *)
            export BASE_VARIANT="bookworm"  # Safe default
            ;;
    esac
else
    export BASE_VARIANT="bookworm"  # Fallback if /etc/os-release doesn't exist
fi

echo "🔍 Detected OS: ${ID:-unknown} ${VERSION_CODENAME:-unknown}"
echo "📦 Using BASE_VARIANT: $BASE_VARIANT"
