#!/usr/bin/env bash
# Copyright (c) 2026 ManuGH
# Licensed under the PolyForm Noncommercial License 1.0.0
#
# The archived Step-5d transport, through both audio paths, compared.
#
# Neither side is told what the other answered. Each writes two files per case -
# the feed boundaries as text, the elementary stream bytes as bytes - and the
# comparison is a diff and a cmp. A digest would only prove the bytes agree if
# the digest itself were right, and two implementations of one digest is one
# more thing that can be wrong in the same direction.
#
# The captures are never re-captured and never modified. They are read from
# XG2G_AUDIO_HARDWARE_DIR and their identity is checked against the Step-5d
# manifest by the Go side before a byte of it is interpreted.
#
# Usage:
#   XG2G_AUDIO_HARDWARE_DIR=/srv/xg2g-5d-captures \
#   XG2G_AUDIO_HARDWARE_OUT=/tmp/6c-traces \
#   media-core/scripts/audio-hardware-differential.sh

set -uo pipefail

cd "$(dirname "$0")/../.." || exit 1

: "${XG2G_AUDIO_HARDWARE_DIR:?name the capture archive}"
: "${XG2G_AUDIO_HARDWARE_OUT:?name where the traces go}"

export XG2G_AUDIO_HARDWARE_DIR XG2G_AUDIO_HARDWARE_OUT
mkdir -p "$XG2G_AUDIO_HARDWARE_OUT"
rm -f "$XG2G_AUDIO_HARDWARE_OUT"/*.trace "$XG2G_AUDIO_HARDWARE_OUT"/*.es

echo "archive: $XG2G_AUDIO_HARDWARE_DIR"
echo "traces:  $XG2G_AUDIO_HARDWARE_OUT"
echo

echo "== the Rust ingress =="
if ! (cd media-core && cargo test --release --test audio_hardware -- --nocapture); then
  echo "the Rust replay failed" >&2
  exit 1
fi

echo
echo "== the Go reference =="
if ! (cd backend && go test ./internal/stream/ingest/mediafacts/ -count=1 -run TestAudioTSHardware -v 2>&1 | grep -Ev '^(=== RUN|=== PAUSE|=== CONT)'); then
  echo "the Go replay failed" >&2
  exit 1
fi

echo
echo "== the comparison =="
cases=0
mismatches=0
for go_trace in "$XG2G_AUDIO_HARDWARE_OUT"/*.go.trace; do
  [ -e "$go_trace" ] || { echo "the Go side wrote no traces" >&2; exit 1; }
  name=$(basename "$go_trace" .go.trace)
  rust_trace="$XG2G_AUDIO_HARDWARE_OUT/$name.rust.trace"
  if [ ! -e "$rust_trace" ]; then
    echo "  $name: the Rust side wrote no trace"
    mismatches=$((mismatches + 1))
    continue
  fi
  cases=$((cases + 1))

  # The stream lines carry counters only one side keeps, so the boundaries are
  # compared on the feed lines and the observations on the stream lines that
  # both sides write.
  go_feeds=$(grep -c '^feed ' "$go_trace")
  rust_feeds=$(grep -c '^feed ' "$rust_trace")
  go_bytes=$(wc -c < "$XG2G_AUDIO_HARDWARE_OUT/$name.go.es" | tr -d ' ')
  rust_bytes=$(wc -c < "$XG2G_AUDIO_HARDWARE_OUT/$name.rust.es" | tr -d ' ')

  bad=""
  if ! diff -q <(grep '^feed ' "$go_trace") <(grep '^feed ' "$rust_trace") >/dev/null; then
    bad="$bad boundaries"
  fi
  if ! diff -q <(grep '^stream ' "$go_trace") <(grep '^stream ' "$rust_trace") >/dev/null; then
    bad="$bad observations"
  fi
  if ! cmp -s "$XG2G_AUDIO_HARDWARE_OUT/$name.go.es" "$XG2G_AUDIO_HARDWARE_OUT/$name.rust.es"; then
    bad="$bad bytes"
  fi

  if [ -n "$bad" ]; then
    printf '  %-20s MISMATCH:%s  (go %s feeds / %s B, rust %s feeds / %s B)\n' \
      "$name" "$bad" "$go_feeds" "$go_bytes" "$rust_feeds" "$rust_bytes"
    mismatches=$((mismatches + 1))
  else
    printf '  %-20s exact   %s feeds, %s elementary stream bytes\n' "$name" "$go_feeds" "$go_bytes"
  fi
done

echo
echo "== what the ingress measured =="
grep -h '^counts ' "$XG2G_AUDIO_HARDWARE_OUT"/*.rust.trace 2>/dev/null | sort | uniq -c | sed 's/^/  /'
echo
echo "== what the streams said =="
for t in "$XG2G_AUDIO_HARDWARE_OUT"/*.rust.trace; do
  printf '  %s\n' "$(basename "$t" .rust.trace)"
  grep '^stream ' "$t" | sed 's/^/    /'
done

echo
echo "cases:      $cases"
echo "mismatches: $mismatches"

if [ "$cases" -eq 0 ]; then
  echo; echo "no case was compared." >&2; exit 1
fi
if [ "$mismatches" -ne 0 ]; then
  exit 1
fi
