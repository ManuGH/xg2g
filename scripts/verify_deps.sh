#!/bin/bash
set -e

# Define Legacy Roots (Must not appear in dependency tree of new domains)
LEGACY_ROOTS=(
    "github.com/ManuGH/xg2g/internal/pipeline"
    "github.com/ManuGH/xg2g/internal/api"
    "github.com/ManuGH/xg2g/internal/vod"
    "github.com/ManuGH/xg2g/internal/epg"
    "github.com/ManuGH/xg2g/internal/library"
)

# Define New Domain Root
NEW_DOMAIN_ROOT="./internal/domain/..."

echo "Checking Transitive Dependencies..."
found_violation=0

# 1. Get all packages in new domain
PACKAGES=$(go list $NEW_DOMAIN_ROOT 2>/dev/null || true)
if [ -z "$PACKAGES" ]; then
    echo "No domain packages found yet. (Pass)"
    exit 0
fi

for pkg in $PACKAGES; do
    echo "Scanning $pkg..."

    # 2. Get Transitive Dependencies (Prod + Test)
    # .Imports = Production imports
    # .TestImports = Test imports
    # .XTestImports = Blackbox test imports
    # Use -deps to recursively find dependencies
    # We use go list -deps -f '{{.ImportPath}}' <pkg> for prod deps
    
    # PROD DEPS
    prod_deps=$(go list -deps -f '{{.ImportPath}}' $pkg 2>/dev/null)
    
    # TEST DEPS (Direct only, verifying transitive test deps is heavy but safer for this stage)
    # For now, let's scan direct Test/XTest imports to catch direct legacy usage in tests.
    test_imports=$(go list -f '{{.TestImports}} {{.XTestImports}}' $pkg | tr -d '[]')
    
    all_refs="$prod_deps $test_imports"

    # Define Whitelist (Transitional allowed dependencies during Strangler migration)
    # Format: "pkg_path:legacy_prefix"
    ALLOWED_VIOLATIONS=(
        "github.com/ManuGH/xg2g/internal/domain/session/manager:github.com/ManuGH/xg2g/internal/pipeline"
    )

    # 0. Safety Guard: Prevent silent whitelist growth
    # CTO Constraint: The whitelist must explicitly be updated/approved if it grows.
    # Current Limit: 1 (Session Manager Move)
    if [ "${#ALLOWED_VIOLATIONS[@]}" -gt 1 ]; then
        echo "❌ VIOLATION: ALLOWED_VIOLATIONS whitelist exceeds approved limit (1)."
        echo "   Reducing debt is the goal. Do not add new waivers without CTO approval."
        exit 1
    fi

    for ref in $all_refs; do
        for legacy in "${LEGACY_ROOTS[@]}"; do
             if [[ "$ref" == "$legacy"* ]]; then
                # Check whitelist
                is_allowed=0
                for allowed in "${ALLOWED_VIOLATIONS[@]}"; do
                    if [[ "$pkg:$ref" == "$allowed"* ]]; then
                        is_allowed=1
                        break
                    fi
                done

                if [ $is_allowed -eq 0 ]; then
                    echo "❌ VIOLATION: Domain package '$pkg' depends on legacy '$ref'"
                    found_violation=1
                fi
            fi
        done
    done
done

if [ $found_violation -eq 1 ]; then
    echo "FAILED: New domain code imports legacy code."
    exit 1
fi

echo "✅ Dependency check passed."
exit 0
