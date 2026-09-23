#!/usr/bin/env bash
#
# Prints the simulator the iOS gate scripts build and test on, as a name or UDID.
#
# An explicit choice always wins: the first argument, then IOS_SIMULATOR. Both
# are passed through untouched.
#
# Otherwise an available iPhone simulator is picked and printed as a UDID. A
# fixed model name does not survive Xcode updates: xcodebuild resolves `name=`
# only on the newest iOS it supports (OS:latest), and each release drops older
# models from that runtime. Xcode 27 ships iOS 27.0 without the iPhone 17 Pro
# that used to be hardcoded here, and the gate failed before compiling anything.
#
# Usage: ios/scripts/select-simulator.sh [simulator name or UDID]

set -euo pipefail

# The reference device. A preference, not a requirement: on a runtime that no
# longer offers it, another iPhone on that runtime is used instead.
PREFERRED_MODEL="iPhone 18 Pro"

if [[ -n "${1:-}" ]]; then
  printf '%s\n' "$1"
  exit 0
fi

if [[ -n "${IOS_SIMULATOR:-}" ]]; then
  printf '%s\n' "${IOS_SIMULATOR}"
  exit 0
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "❌ jq is required to select a simulator automatically (macOS 15+ ships /usr/bin/jq)" >&2
  echo "   Pass a simulator name or UDID, or set IOS_SIMULATOR." >&2
  exit 1
fi

if ! SIMCTL_JSON="$(xcrun simctl list --json)"; then
  echo "❌ xcrun simctl list failed; cannot select a simulator" >&2
  exit 1
fi

# OS:latest is "the most recent version of iOS supported by this version of
# Xcode", i.e. its simulator SDK. A newer runtime, left behind by another Xcode
# installed side by side, is used only when no other runtime has an iPhone.
SDK_VERSION="$(xcrun --sdk iphonesimulator --show-sdk-version 2>/dev/null || true)"

# Order: runtimes this Xcode supports first, then newest runtime, then the
# preferred model, then simctl's own listing order.
if ! SELECTION="$(jq -r --arg preferred "${PREFERRED_MODEL}" --arg sdk "${SDK_VERSION}" '
  def version: (split(".") | map(tonumber? // 0)) + [0, 0, 0] | .[0:3];
  (if $sdk == "" then null else $sdk | version | .[0:2] end) as $max_version
  | (reduce (.devicetypes[] | select(.productFamily == "iPhone")) as $type
      ({}; .[$type.identifier] = true)) as $iphone
  | .devices as $devices
  | [ .runtimes[] | select(.isAvailable and .platform == "iOS") as $runtime
      | ($devices[$runtime.identifier] // []) | to_entries[]
      | select(.value.isAvailable and $iphone[.value.deviceTypeIdentifier])
      | { udid: .value.udid, name: .value.name, runtime: $runtime.name,
          version: ($runtime.version | version), order: .key } ]
  | sort_by([
      (if $max_version == null or .version[0:2] <= $max_version then 0 else 1 end),
      (.version | map(0 - .)),
      (if .name == $preferred then 0 else 1 end),
      .order ])
  | first // empty
  | [.udid, .name, .runtime] | @tsv
' <<<"${SIMCTL_JSON}")"; then
  echo "❌ could not read the simulator list from xcrun simctl" >&2
  exit 1
fi

if [[ -z "${SELECTION}" ]]; then
  echo "❌ no available iPhone simulator on any installed iOS runtime" >&2
  echo "   Install one (Xcode > Settings > Components, or 'xcodebuild -downloadPlatform iOS')," >&2
  echo "   or pass a simulator name or UDID, or set IOS_SIMULATOR." >&2
  exit 1
fi

IFS=$'\t' read -r UDID NAME RUNTIME <<<"${SELECTION}"
echo "==> Selected simulator ${NAME} (${RUNTIME}, ${UDID}); pass a name or UDID, or set IOS_SIMULATOR, to override" >&2
printf '%s\n' "${UDID}"
