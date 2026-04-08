#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${XG2G_ENV_FILE:-/etc/xg2g/xg2g.env}"
PLAYLIST_FILE="${XG2G_PLAYLIST_FILE:-/var/lib/xg2g/playlist.m3u}"
CONTAINER_NAME="${XG2G_CONTAINER_NAME:-xg2g}"
READY_TIMEOUT_SEC="${XG2G_POST_DEPLOY_READY_TIMEOUT_SEC:-60}"
SERVICE_NAME_OVERRIDE="${XG2G_POST_DEPLOY_SERVICE_NAME:-}"
SERVICE_REF_OVERRIDE="${XG2G_POST_DEPLOY_SERVICE_REF:-}"
CANONICAL_ENV_FILE="/etc/xg2g/xg2g.env"
TMPDIR_ROOT="$(mktemp -d)"
STARTED_SESSIONS=()

cleanup() {
  local sid
  for sid in "${STARTED_SESSIONS[@]:-}"; do
    stop_session "$sid" >/dev/null 2>&1 || true
  done
  rm -rf "${TMPDIR_ROOT}"
}
trap cleanup EXIT

fail() {
  echo "❌ $*" >&2
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

trim_ascii_whitespace() {
  local value="$1"

  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "${value}"
}

stat_mode() {
  local path="$1"

  if stat -c '%a' "${path}" >/dev/null 2>&1; then
    stat -c '%a' "${path}"
    return 0
  fi
  stat -f '%Lp' "${path}"
}

stat_owner() {
  local path="$1"

  if stat -c '%u:%g' "${path}" >/dev/null 2>&1; then
    stat -c '%u:%g' "${path}"
    return 0
  fi
  stat -f '%u:%g' "${path}"
}

assert_secure_env_file() {
  local path="$1"
  local mode owner

  [[ "${path}" == "${CANONICAL_ENV_FILE}" ]] || return 0

  mode="$(stat_mode "${path}")"
  [[ "${mode}" == "600" ]] || fail "insecure ${path} mode ${mode}; expected 600"

  owner="$(stat_owner "${path}")"
  [[ "${owner}" == "0:0" ]] || fail "insecure ${path} owner ${owner}; expected 0:0 (root:root)"
}

read_env_value() {
  local env_file="$1"
  local wanted="$2"
  local raw line key value first_char last_char

  [[ -f "${env_file}" ]] || return 1

  while IFS= read -r raw || [[ -n "${raw}" ]]; do
    line="$(trim_ascii_whitespace "${raw}")"
    [[ -n "${line}" ]] || continue
    [[ "${line:0:1}" == "#" ]] && continue

    if [[ ! "${line}" =~ ^(export[[:space:]]+)?([A-Za-z_][A-Za-z0-9_]*)[[:space:]]*=(.*)$ ]]; then
      continue
    fi

    key="${BASH_REMATCH[2]}"
    [[ "${key}" == "${wanted}" ]] || continue

    value="$(trim_ascii_whitespace "${BASH_REMATCH[3]}")"
    if [[ ${#value} -ge 2 ]]; then
      first_char="${value:0:1}"
      last_char="${value: -1}"
      if [[ ("${first_char}" == '"' || "${first_char}" == "'") && "${last_char}" == "${first_char}" ]]; then
        printf '%s\n' "${value:1:${#value}-2}"
        return 0
      fi
    fi

    if [[ "${value}" =~ ^#.*$ ]]; then
      printf '\n'
      return 0
    fi
    if [[ "${value}" =~ ^(.*[^[:space:]])[[:space:]]+#.*$ ]]; then
      value="${BASH_REMATCH[1]}"
    fi

    printf '%s\n' "$(trim_ascii_whitespace "${value}")"
    return 0
  done < "${env_file}"

  return 1
}

for cmd in curl jq docker awk sed grep base64; do
  need_cmd "$cmd"
done

if [[ "${EUID}" -ne 0 ]]; then
  fail "must run as root (needs ${ENV_FILE} and docker access)"
fi

if [[ -f "${ENV_FILE}" ]]; then
  assert_secure_env_file "${ENV_FILE}"
  if api_token_from_file="$(read_env_value "${ENV_FILE}" XG2G_API_TOKEN 2>/dev/null)"; then
    XG2G_API_TOKEN="${api_token_from_file}"
  fi
  if listen_from_file="$(read_env_value "${ENV_FILE}" XG2G_LISTEN 2>/dev/null)"; then
    XG2G_LISTEN="${listen_from_file}"
  fi
fi

API_TOKEN="${XG2G_API_TOKEN:-}"
[[ -n "${API_TOKEN}" ]] || fail "XG2G_API_TOKEN is required (load ${ENV_FILE} or export it)"

normalize_expected_encoder_backend() {
  local backend="${XG2G_POST_DEPLOY_EXPECT_ENCODER_BACKEND:-auto}"
  backend="${backend,,}"

  case "${backend}" in
    ""|auto)
      printf 'auto'
      ;;
    vaapi|nvenc)
      printf '%s' "${backend}"
      ;;
    *)
      fail "invalid XG2G_POST_DEPLOY_EXPECT_ENCODER_BACKEND=${backend} (expected auto, vaapi, or nvenc)"
      ;;
  esac
}

EXPECTED_ENCODER_BACKEND="$(normalize_expected_encoder_backend)"

resolve_api_origin() {
  local listen="${1:-127.0.0.1:8088}"
  local host port

  listen="${listen#http://}"
  listen="${listen#https://}"

  if [[ "${listen}" == \[*\]:* ]]; then
    port="${listen##*:}"
    host="127.0.0.1"
  elif [[ "${listen}" == *:* ]]; then
    host="${listen%:*}"
    port="${listen##*:}"
  else
    host="${listen}"
    port="8088"
  fi

  case "${host}" in
    ""|"0.0.0.0"|"::"|"[::]")
      host="127.0.0.1"
      ;;
  esac

  printf 'http://%s:%s' "${host}" "${port}"
}

API_ORIGIN="$(resolve_api_origin "${XG2G_LISTEN:-127.0.0.1:8088}")"
API_BASE="${API_ORIGIN}/api/v3"
REFERER="${API_ORIGIN}/"

join_url() {
  local base="$1"
  local path="$2"
  if [[ "${path}" == http://* || "${path}" == https://* ]]; then
    printf '%s' "${path}"
  else
    printf '%s%s' "${base%/}" "${path}"
  fi
}

url_decode() {
  local raw="${1//+/ }"
  printf '%b' "${raw//%/\\x}"
}

discover_service() {
  local match_name="${1:-}"
  local encoded_ref=""
  local service_name=""

  [[ -f "${PLAYLIST_FILE}" ]] || fail "playlist file not found: ${PLAYLIST_FILE}"

  while IFS= read -r line; do
    if [[ "${line}" == \#EXTINF:* ]]; then
      service_name="${line##*,}"
      continue
    fi
    if [[ "${line}" == http* ]] && [[ "${line}" == *"ref="* ]]; then
      encoded_ref="${line#*ref=}"
      encoded_ref="${encoded_ref%%&*}"
      if [[ -z "${match_name}" ]] || [[ "${service_name,,}" == *"${match_name,,}"* ]]; then
        printf '%s\t%s\n' "$(url_decode "${encoded_ref}")" "${service_name}"
        return 0
      fi
    fi
  done < "${PLAYLIST_FILE}"

  return 1
}

if [[ -n "${SERVICE_REF_OVERRIDE}" ]]; then
  SERVICE_REF="${SERVICE_REF_OVERRIDE}"
  SERVICE_NAME="${SERVICE_NAME_OVERRIDE:-custom}"
else
  service_row="$(discover_service "${SERVICE_NAME_OVERRIDE}")" || fail "failed to discover service ref from ${PLAYLIST_FILE}"
  SERVICE_REF="${service_row%%$'\t'*}"
  SERVICE_NAME="${service_row#*$'\t'}"
fi

curl_json() {
  local method="$1"
  local url="$2"
  local body="${3:-}"
  local body_file="${TMPDIR_ROOT}/body.$RANDOM.json"
  local header_file="${TMPDIR_ROOT}/headers.$RANDOM.txt"
  local status

  if [[ -n "${body}" ]]; then
    curl -sS -D "${header_file}" -o "${body_file}" \
      -X "${method}" \
      -H "Authorization: Bearer ${API_TOKEN}" \
      -H 'Content-Type: application/json' \
      -H "Origin: ${API_ORIGIN}" \
      -H "Referer: ${REFERER}" \
      --data "${body}" \
      "${url}"
  else
    curl -sS -D "${header_file}" -o "${body_file}" \
      -X "${method}" \
      -H "Authorization: Bearer ${API_TOKEN}" \
      -H "Origin: ${API_ORIGIN}" \
      -H "Referer: ${REFERER}" \
      "${url}"
  fi

  status="$(awk '/^HTTP/{code=$2} END{print code}' "${header_file}")"
  CURL_STATUS="${status}"
  CURL_REQUEST_ID="$(awk 'BEGIN{IGNORECASE=1} /^X-Request-ID:/{print $2}' "${header_file}" | tr -d '\r' | tail -n1)"
  CURL_BODY="$(cat "${body_file}")"
}

stop_session() {
  local sid="$1"
  [[ -n "${sid}" ]] || return 0
  curl_json "POST" "${API_BASE}/intents" "$(jq -nc --arg sid "${sid}" '{type:"stream.stop",sessionId:$sid}')"
}

jwt_payload_json() {
  local token="$1"
  local payload="${token#*.}"
  payload="${payload%%.*}"
  case $((${#payload} % 4)) in
    2) payload="${payload}==" ;;
    3) payload="${payload}=" ;;
  esac
  printf '%s' "${payload}" | tr '_-' '/+' | base64 -d 2>/dev/null
}

wait_for_session_ready() {
  local sid="$1"
  local deadline=$((SECONDS + READY_TIMEOUT_SEC))
  local state=""

  while (( SECONDS < deadline )); do
    curl_json "GET" "${API_BASE}/sessions/${sid}"
    [[ "${CURL_STATUS}" == "200" ]] || fail "session ${sid} returned HTTP ${CURL_STATUS}: ${CURL_BODY}"

    state="$(printf '%s' "${CURL_BODY}" | jq -r '.state // empty')"
    case "${state}" in
      READY|DRAINING)
        if printf '%s' "${CURL_BODY}" | jq -e '.playbackUrl // empty' >/dev/null; then
          printf '%s' "${CURL_BODY}"
          return 0
        fi
        ;;
      FAILED|STOPPED|STOPPING|CANCELLED)
        fail "session ${sid} entered terminal state ${state}: ${CURL_BODY}"
        ;;
    esac
    sleep 1
  done

  fail "session ${sid} did not become READY within ${READY_TIMEOUT_SEC}s"
}

fetch_manifest_without_auth() {
  local manifest_url="$1"
  local body_file="${TMPDIR_ROOT}/manifest.$RANDOM.m3u8"
  local header_file="${TMPDIR_ROOT}/manifest.$RANDOM.headers"

  curl -sS -D "${header_file}" -o "${body_file}" "${manifest_url}"
  MANIFEST_STATUS="$(awk '/^HTTP/{code=$2} END{print code}' "${header_file}")"
  MANIFEST_BODY="$(cat "${body_file}")"
}

verify_direct_live_hls() {
  local caps info_body token mode cap_hash playback_mode start_body sid session_json playback_url manifest_url

  caps="$(jq -nc '{
    capabilitiesVersion: 2,
    container: ["mp4","ts"],
    videoCodecs: ["h264"],
    audioCodecs: ["aac","mp3","ac3"],
    supportsHls: true,
    supportsRange: true,
    deviceType: "web",
    hlsEngines: ["hlsjs"],
    preferredHlsEngine: "hlsjs",
    runtimeProbeUsed: true,
    runtimeProbeVersion: 1,
    clientFamilyFallback: "chrome"
  }')"
  info_body="$(jq -nc --arg ref "${SERVICE_REF}" --argjson caps "${caps}" '{serviceRef:$ref,capabilities:$caps}')"
  curl_json "POST" "${API_BASE}/live/stream-info" "${info_body}"
  [[ "${CURL_STATUS}" == "200" ]] || fail "live/stream-info failed: HTTP ${CURL_STATUS}: ${CURL_BODY}"

  token="$(printf '%s' "${CURL_BODY}" | jq -r '.playbackDecisionToken // empty')"
  mode="$(printf '%s' "${CURL_BODY}" | jq -r '.mode // empty')"
  [[ -n "${token}" ]] || fail "live/stream-info returned no playbackDecisionToken"

  cap_hash="$(jwt_payload_json "${token}" | jq -r '.capHash // empty')"
  case "${mode}" in
    native_hls) playback_mode="native_hls" ;;
    hls|direct_stream|hlsjs) playback_mode="hlsjs" ;;
    transcode) playback_mode="transcode" ;;
    direct_mp4) playback_mode="direct_mp4" ;;
    *) fail "unsupported live mode from stream-info: ${mode}" ;;
  esac

  start_body="$(jq -nc \
    --arg ref "${SERVICE_REF}" \
    --arg tok "${token}" \
    --arg playback_mode "${playback_mode}" \
    --arg cap_hash "${cap_hash}" \
    '{
      type:"stream.start",
      serviceRef:$ref,
      playbackDecisionToken:$tok,
      params: (
        {playback_mode:$playback_mode, playback_decision_token:$tok}
        + (if $cap_hash != "" then {capHash:$cap_hash} else {} end)
      )
    }')"
  curl_json "POST" "${API_BASE}/intents" "${start_body}"
  [[ "${CURL_STATUS}" == "202" ]] || fail "direct live intent failed: HTTP ${CURL_STATUS}: ${CURL_BODY}"
  sid="$(printf '%s' "${CURL_BODY}" | jq -r '.sessionId // empty')"
  [[ -n "${sid}" ]] || fail "direct live intent returned no sessionId"
  STARTED_SESSIONS+=("${sid}")

  session_json="$(wait_for_session_ready "${sid}")"
  playback_url="$(printf '%s' "${session_json}" | jq -r '.playbackUrl // empty')"
  [[ "${playback_url}" == *"token="* ]] || fail "playbackUrl missing token query parameter: ${playback_url}"

  manifest_url="$(join_url "${API_ORIGIN}" "${playback_url}")"
  fetch_manifest_without_auth "${manifest_url}"
  [[ "${MANIFEST_STATUS}" == "200" ]] || fail "manifest fetch without auth failed: HTTP ${MANIFEST_STATUS}"

  if printf '%s\n' "${MANIFEST_BODY}" | grep -q '^#EXT-X-PLAYLIST-TYPE:'; then
    fail "live manifest still emits EXT-X-PLAYLIST-TYPE"
  fi
  printf '%s\n' "${MANIFEST_BODY}" | grep -q '^#EXT-X-START:' || fail "live manifest missing EXT-X-START"
  printf '%s\n' "${MANIFEST_BODY}" | grep -q '^#EXT-X-PROGRAM-DATE-TIME:' || fail "live manifest missing EXT-X-PROGRAM-DATE-TIME"
  printf '%s\n' "${MANIFEST_BODY}" | grep -Eq '(init\.mp4|seg_[0-9]+).*(\?|&)token=' || fail "manifest entries missing propagated media token"

  echo "✅ Live HLS tokenized manifest OK (${sid}, ${SERVICE_NAME})"
}

verify_hw_transcode_gpu() {
  local caps info_body token cap_hash start_body sid session_json request_id intent_request_id intent_log_line
  local pipeline_log_line ffmpeg_line encoder_backend expected_video_encoder pipeline_pattern

  caps="$(jq -nc '{
    capabilitiesVersion: 2,
    container: ["mp4","ts"],
    videoCodecs: ["h264"],
    audioCodecs: ["aac","mp3","ac3"],
    supportsHls: true,
    supportsRange: true,
    deviceType: "web",
    hlsEngines: ["hlsjs"],
    preferredHlsEngine: "hlsjs",
    runtimeProbeUsed: true,
    runtimeProbeVersion: 1,
    clientFamilyFallback: "chrome"
  }')"
  info_body="$(jq -nc --arg ref "${SERVICE_REF}" --argjson caps "${caps}" '{serviceRef:$ref,capabilities:$caps}')"
  curl_json "POST" "${API_BASE}/live/stream-info" "${info_body}"
  [[ "${CURL_STATUS}" == "200" ]] || fail "transcode preflight stream-info failed: HTTP ${CURL_STATUS}: ${CURL_BODY}"
  token="$(printf '%s' "${CURL_BODY}" | jq -r '.playbackDecisionToken // empty')"
  [[ -n "${token}" ]] || fail "transcode preflight stream-info returned no playbackDecisionToken"
  cap_hash="$(jwt_payload_json "${token}" | jq -r '.capHash // empty')"

  start_body="$(jq -nc \
    --arg ref "${SERVICE_REF}" \
    --arg tok "${token}" \
    --arg cap_hash "${cap_hash}" \
    '{
      type:"stream.start",
      serviceRef:$ref,
      playbackDecisionToken:$tok,
      params:{
        playback_mode:"transcode",
        playback_decision_token:$tok,
        capHash:$cap_hash,
        hwaccel:"force"
      }
    }')"
  curl_json "POST" "${API_BASE}/intents" "${start_body}"
  [[ "${CURL_STATUS}" == "202" ]] || fail "forced transcode intent failed: HTTP ${CURL_STATUS}: ${CURL_BODY}"
  sid="$(printf '%s' "${CURL_BODY}" | jq -r '.sessionId // empty')"
  intent_request_id="${CURL_REQUEST_ID:-}"
  [[ -n "${sid}" ]] || fail "forced transcode intent returned no sessionId"
  STARTED_SESSIONS+=("${sid}")

  session_json="$(wait_for_session_ready "${sid}")"
  request_id="$(printf '%s' "${session_json}" | jq -r '.requestId // empty')"
  sleep 2

  intent_log_line="$(docker logs --since 5m "${CONTAINER_NAME}" 2>&1 | grep -F "${intent_request_id}" | grep 'intent profile resolved' | tail -n1 || true)"
  [[ -n "${intent_log_line}" ]] || fail "missing intent profile resolved log for request ${intent_request_id}"
  printf '%s\n' "${intent_log_line}" | grep -q '"gpu_available":true' || fail "intent log missing gpu_available=true"
  printf '%s\n' "${intent_log_line}" | grep -q '"hwaccel_effective":"gpu"' || fail "intent log missing hwaccel_effective=gpu"
  encoder_backend="$(printf '%s\n' "${intent_log_line}" | jq -r '.encoder_backend // empty')"
  case "${encoder_backend}" in
    vaapi)
      expected_video_encoder="h264_vaapi"
      pipeline_pattern='pipeline video: vaapi'
      ;;
    nvenc)
      expected_video_encoder="h264_nvenc"
      pipeline_pattern='pipeline video: nvenc'
      ;;
    *)
      fail "intent log reported unexpected encoder_backend=${encoder_backend:-empty}"
      ;;
  esac
  if [[ "${EXPECTED_ENCODER_BACKEND}" != "auto" && "${encoder_backend}" != "${EXPECTED_ENCODER_BACKEND}" ]]; then
    fail "intent log selected encoder_backend=${encoder_backend}, expected ${EXPECTED_ENCODER_BACKEND}"
  fi

  pipeline_log_line="$(docker logs --since 5m "${CONTAINER_NAME}" 2>&1 | grep -F "\"sessionId\":\"${sid}\"" | grep "${pipeline_pattern}" | tail -n1 || true)"
  [[ -n "${pipeline_log_line}" ]] || fail "missing ${pipeline_pattern} log for session ${sid}"

  ffmpeg_line="$(docker top "${CONTAINER_NAME}" -eo pid,args | grep -F "${sid}" | grep -F '/opt/ffmpeg/bin/ffmpeg' | tail -n1 || true)"
  [[ -n "${ffmpeg_line}" ]] || fail "missing ffmpeg process line for session ${sid}"
  printf '%s\n' "${ffmpeg_line}" | grep -q -- "-c:v ${expected_video_encoder}" || fail "ffmpeg line missing ${expected_video_encoder}"
  case "${encoder_backend}" in
    vaapi)
      printf '%s\n' "${ffmpeg_line}" | grep -Eq -- '-vaapi_device /dev/dri/renderD[0-9]+' || fail "ffmpeg line missing -vaapi_device /dev/dri/renderD*"
      ;;
    nvenc)
      if printf '%s\n' "${ffmpeg_line}" | grep -q -- '-vaapi_device '; then
        fail "nvenc ffmpeg line unexpectedly contains -vaapi_device"
      fi
      ;;
  esac
  if printf '%s\n' "${pipeline_log_line}" | grep -q '"deinterlace":true'; then
    printf '%s\n' "${ffmpeg_line}" | grep -Eq -- 'deinterlace_vaapi|yadif|bwdif=mode=send_field:parity=auto:deint=all' || fail "ffmpeg line missing expected deinterlace filter despite deinterlace=true"
  fi

  echo "✅ Forced ${encoder_backend^^} transcode OK (${sid}, request ${request_id:-unknown})"
}

echo "== Post-deploy playback verifier =="
echo "API base: ${API_BASE}"
echo "Service: ${SERVICE_NAME} (${SERVICE_REF})"

docker inspect -f '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{else}}no-health{{end}}' "${CONTAINER_NAME}" \
  | grep -q '^running healthy$' || fail "container ${CONTAINER_NAME} is not running healthy"

verify_direct_live_hls
verify_hw_transcode_gpu

echo "OK: post-deploy playback verifier passed."
