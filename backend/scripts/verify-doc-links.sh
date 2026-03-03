#!/usr/bin/env bash
set -euo pipefail

# verify-doc-links.sh
# Stop-the-line doc hygiene gate: fail if docs/**/*.md contains broken relative links.
#
# Must:
# - Scan docs/**/*.md deterministically (via git ls-files)
# - Find relative links/images in Markdown
# - Ignore: http(s), mailto, tel, pure anchors, code blocks
# - Support: path.md#anchor (checks file exists), URL-encoded paths
# - Fail-closed: verify files exist before reading (prevent git index inconsistency)
#
# Non-goals:
# - Anchor existence check (too expensive/fragile)
# - HTML link parsing (Markdown only)

ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

echo "--- Checking Docs: Broken Relative Links ---"

DOC_FILES="$(git ls-files 'docs/**/*.md' || true)"
if [[ -z "${DOC_FILES}" ]]; then
  echo "No docs/**/*.md files found."
  exit 0
fi

BROKEN=0
CHECKED=0

# Helper: trim surrounding whitespace
trim() { sed -E 's/^[[:space:]]+|[[:space:]]+$//g'; }

# Iterate files safely line-by-line
while IFS= read -r FILE; do
  [[ -z "$FILE" ]] && continue

  # Fail-closed: verify file exists in working tree before reading
  if [[ ! -f "$FILE" ]]; then
    echo "❌ INTERNAL ERROR: git ls-files returned non-existent file: $FILE"
    echo "   This indicates git index inconsistency (deleted but not committed, or rename in progress)."
    echo "   Run 'git status' and commit or stash changes before re-running."
    exit 1
  fi

  # Read file line-by-line; skip fenced code blocks
  in_code=0
  line_no=0

  while IFS= read -r line || [[ -n "$line" ]]; do
    line_no=$((line_no + 1))

    # Toggle fenced code blocks (``` or ~~~)
    if [[ "$line" =~ ^\`\`\` ]] || [[ "$line" =~ ^\~\~\~ ]]; then
      if [[ $in_code -eq 0 ]]; then in_code=1; else in_code=0; fi
      continue
    fi
    [[ $in_code -eq 1 ]] && continue

    # Extract markdown link targets from (...) after ] or !]
    # shellcheck disable=SC2016
    while IFS= read -r target; do
      target="$(printf "%s" "$target" | trim)"

      # Skip empty
      [[ -z "$target" ]] && continue

      # Strip optional title: (path "title") or (path 'title')
      target="$(printf "%s" "$target" | sed -E 's/[[:space:]]+(".*"|'"'"'.*'"'"')[[:space:]]*$//')"

      # Skip external schemes and pure anchors
      if [[ "$target" =~ ^https?:// ]] || [[ "$target" =~ ^mailto: ]] || [[ "$target" =~ ^tel: ]] || [[ "$target" =~ ^file:// ]]; then
        continue
      fi
      if [[ "$target" =~ ^# ]]; then
        continue
      fi

      # Remove fragment
      path="${target%%#*}"

      # URL decode minimal (%20 etc.)
      path_decoded="$(printf '%b' "${path//%/\\x}")"

      # Normalize: ignore querystring if present
      path_decoded="${path_decoded%%\?*}"

      # Only validate relative or docs-local paths (no absolute filesystem)
      if [[ "$path_decoded" =~ ^/ ]]; then
        # Treat as repo-relative absolute path; check relative to root.
        resolved="$ROOT${path_decoded}"
      else
        # Resolve relative to current md file directory
        base_dir="$(dirname "$FILE")"
        resolved="$ROOT/$base_dir/$path_decoded"
      fi

      # Collapse .. and .
      if command -v realpath >/dev/null 2>&1; then
        resolved_norm="$(realpath -m "$resolved" 2>/dev/null || true)"
      else
        # Fallback: best-effort normalization without realpath
        resolved_norm="$resolved"
      fi

      CHECKED=$((CHECKED + 1))

      if [[ ! -e "$resolved_norm" ]]; then
        BROKEN=$((BROKEN + 1))
        echo "❌ $FILE:$line_no: broken link -> $target (resolved: ${resolved_norm#$ROOT/})"
      fi
    done < <(
      # Extract targets inside (...) for markdown links/images.
      # This ignores nested parentheses by design.
      printf "%s\n" "$line" \
        | grep -oE '(!?\[[^]]*\]\([^)]+\))' \
        | sed -E 's/^.*\(([^)]+)\).*$/\1/' \
        || true
    )
  done < "$FILE"
done <<< "$DOC_FILES"

echo ""
echo "Summary:"
echo "  LINKS_CHECKED=$CHECKED"
echo "  BROKEN_LINKS=$BROKEN"

if [[ $BROKEN -eq 0 ]]; then
  echo "✅ PASS: no broken relative doc links detected"
  exit 0
fi

echo "❌ FAIL: broken doc links detected"
exit 1
