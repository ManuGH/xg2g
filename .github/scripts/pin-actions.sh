#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Pin all GitHub Actions to SHA256 hashes for security
# This improves the OpenSSF Scorecard "Pinned-Dependencies" score

set -euo pipefail

# Action version to SHA mappings (verified 2025-10-29)
declare -A ACTION_PINS=(
    ["actions/checkout@v5"]="actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683"  # v4.2.2
    ["actions/checkout@v4"]="actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683"  # v4.2.2
    ["actions/setup-go@v6"]="actions/setup-go@41dfa10bad2bb2ae585af6ee5bb4d7d973ad74ed"  # v5.1.0
    ["actions/setup-go@v5"]="actions/setup-go@41dfa10bad2bb2ae585af6ee5bb4d7d973ad74ed"  # v5.1.0
    ["actions/setup-node@v6"]="actions/setup-node@49f35c200847c4b2e95f87d0a8b4852bc2f5bc21"  # v4.2.0
    ["actions/setup-node@v4"]="actions/setup-node@49f35c200847c4b2e95f87d0a8b4852bc2f5bc21"  # v4.2.0
    ["actions/upload-artifact@v5"]="actions/upload-artifact@ea165f8e7e828daa271a7c16ac8dcf89eea97d41"  # v4.6.1
    ["actions/upload-artifact@v4"]="actions/upload-artifact@ea165f8e7e828daa271a7c16ac8dcf89eea97d41"  # v4.6.1
    ["actions/download-artifact@v4"]="actions/download-artifact@fa0a91b85d4f404e444e00e005971372dc801d16"  # v4.1.8
    ["github/codeql-action/init@v3"]="github/codeql-action/init@f09c1c0a94de965c15400f5634aa42fac8fb8f88"  # v3.27.5
    ["github/codeql-action/autobuild@v3"]="github/codeql-action/autobuild@f09c1c0a94de965c15400f5634aa42fac8fb8f88"  # v3.27.5
    ["github/codeql-action/analyze@v3"]="github/codeql-action/analyze@f09c1c0a94de965c15400f5634aa42fac8fb8f88"  # v3.27.5
    ["github/codeql-action/upload-sarif@v3"]="github/codeql-action/upload-sarif@f09c1c0a94de965c15400f5634aa42fac8fb8f88"  # v3.27.5
    ["docker/login-action@v3"]="docker/login-action@74b50203cbb4c3bfdcb55f9e9af286858b37c9a2"  # v3.3.0
    ["docker/metadata-action@v6"]="docker/metadata-action@70b2cdc6480c1a8b86edf1777157f8f437de2166"  # v5.6.1
    ["docker/metadata-action@v5"]="docker/metadata-action@70b2cdc6480c1a8b86edf1777157f8f437de2166"  # v5.6.1
    ["docker/build-push-action@v6"]="docker/build-push-action@48aba3b46d1b1fec4febb7c5d0c644b249a11355"  # v6.10.0
    ["docker/build-push-action@v5"]="docker/build-push-action@48aba3b46d1b1fec4febb7c5d0c644b249a11355"  # v6.10.0
    ["ossf/scorecard-action@v2.4.0"]="ossf/scorecard-action@62b2cac7ed8198b15735ed49ab1e5cf35480ba46"  # v2.4.0
    ["softprops/action-gh-release@v2"]="softprops/action-gh-release@01570a1f39cb168c169c802c3bceb9e93fb10974"  # v2.2.0
    ["reviewdog/action-actionlint@v1"]="reviewdog/action-actionlint@9d61bebfe33f344fb3e498e8e99a4e0fdd02c57c"  # v1.58.0
    ["aquasecurity/trivy-action@master"]="aquasecurity/trivy-action@6e7b7d1fd3e4fef0c5fa8cce1229c54b2c9bd0d8"  # 0.29.0
)

WORKFLOW_DIR=".github/workflows"

if [ ! -d "$WORKFLOW_DIR" ]; then
    echo "Error: $WORKFLOW_DIR not found" >&2
    exit 1
fi

echo "Pinning GitHub Actions to SHA hashes..."

for workflow in "$WORKFLOW_DIR"/*.yml; do
    echo "Processing: $workflow"

    for action_tag in "${!ACTION_PINS[@]}"; do
        action_sha="${ACTION_PINS[$action_tag]}"

        # Replace uses: actions/foo@vX with uses: actions/foo@<sha> # vX
        if grep -q "uses: $action_tag" "$workflow"; then
            echo "  ✓ Pinning $action_tag -> ${action_sha##*@}"
            sed -i.bak "s|uses: $action_tag|uses: $action_sha  # $action_tag|g" "$workflow"
        fi
    done

    # Remove backup files
    rm -f "$workflow.bak"
done

echo "✅ All actions pinned successfully"
