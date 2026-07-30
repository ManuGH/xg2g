#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 4 ]]; then
  echo "usage: $0 <raw-profile> <filtered-profile> <text-report> <minimum-percent>" >&2
  exit 2
fi

raw_profile="$1"
filtered_profile="$2"
text_report="$3"
minimum="$4"
go_bin="${GO_BIN:-go}"

[[ -s "${raw_profile}" ]] || {
  echo "coverage profile is missing or empty: ${raw_profile}" >&2
  exit 1
}
head -n 1 "${raw_profile}" | grep -Eq '^mode: (set|count|atomic)$' || {
  echo "coverage profile has an invalid mode header: ${raw_profile}" >&2
  exit 1
}
[[ "${minimum}" =~ ^[0-9]+([.][0-9]+)?$ ]] || {
  echo "coverage minimum must be numeric: ${minimum}" >&2
  exit 2
}
awk -v minimum="${minimum}" 'BEGIN { exit !((minimum + 0) >= 0 && (minimum + 0) <= 100) }' || {
  echo "coverage minimum must be between 0 and 100: ${minimum}" >&2
  exit 2
}

mkdir -p "$(dirname "${filtered_profile}")" "$(dirname "${text_report}")"
head -n 1 "${raw_profile}" > "${filtered_profile}"
awk 'NR > 1 && $1 !~ /_gen\.go:/ { print }' "${raw_profile}" >> "${filtered_profile}"

"${go_bin}" tool cover -func="${filtered_profile}" > "${text_report}"
total="$(
  awk '/^total:/ { value=$3; gsub(/%/, "", value); print value }' "${text_report}"
)"
[[ -n "${total}" ]] || {
  echo "failed to extract total coverage" >&2
  exit 1
}

awk -v total="${total}" -v minimum="${minimum}" 'BEGIN {
  if ((total + 0) < (minimum + 0)) {
    printf("coverage gate failed: total %.2f%% < minimum %.2f%%\n", total, minimum) > "/dev/stderr"
    exit 1
  }
}'

printf 'Coverage gate passed: %s%% >= %s%%\n' "${total}" "${minimum}"
printf 'coverage_total=%s\n' "${total}"
