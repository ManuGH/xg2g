#!/usr/bin/env bash
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
BASE_TAG="${XG2G_VERIFY_BASE_TAG:-xg2g-ffmpeg-base:security-closure}"
RUNTIME_TAG="${XG2G_VERIFY_RUNTIME_TAG:-xg2g:security-closure}"
DISTROLESS_TAG="${XG2G_VERIFY_DISTROLESS_TAG:-xg2g-distroless:security-closure}"
USE_NO_CACHE="${XG2G_VERIFY_NO_CACHE:-1}"
USE_PULL="${XG2G_VERIFY_PULL:-0}"
USE_TRIVY="${XG2G_VERIFY_TRIVY:-1}"

fail() {
	echo "FAIL: $*" >&2
	exit 1
}

log() {
	echo "==> $*"
}

need_cmd() {
	command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

docker_build() {
	local tag="$1"
	shift
	local flags=()
	if [[ "${USE_NO_CACHE}" == "1" ]]; then
		flags+=(--no-cache)
	fi
	if [[ "${USE_PULL}" == "1" ]]; then
		flags+=(--pull)
	fi
	docker build "${flags[@]}" -t "${tag}" "$@"
}

expect_image_user() {
	local image="$1"
	local expected="$2"
	local actual
	actual="$(docker image inspect "${image}" --format '{{.Config.User}}')"
	[[ "${actual}" == "${expected}" ]] || fail "image ${image} has user ${actual}, want ${expected}"
}

verify_runtime_image() {
	local image="$1"

	log "checking runtime image user"
	expect_image_user "${image}" "10001:10001"

	log "checking runtime image CLI under default non-root user"
	docker run --rm --entrypoint xg2g "${image}" help >/dev/null
	docker run --rm --entrypoint xg2g "${image}" healthcheck --help >/dev/null

	log "checking runtime writable paths and ffmpeg wrappers"
	docker run --rm --entrypoint sh "${image}" -lc '
		test "$(id -u)" = "10001"
		test "$(id -g)" = "10001"
		case " $(id -Gn) " in
			*" video "*) ;;
			*) exit 1 ;;
		esac
		test "$PWD" = "/var/lib/xg2g"
		test -d /var/lib/xg2g/tmp
		test -d /var/lib/xg2g/sessions
		test -d /var/lib/xg2g/recordings
		touch /var/lib/xg2g/tmp/.verify && rm /var/lib/xg2g/tmp/.verify
		touch /var/lib/xg2g/sessions/.verify && rm /var/lib/xg2g/sessions/.verify
		touch /var/lib/xg2g/recordings/.verify && rm /var/lib/xg2g/recordings/.verify
		test -x /usr/local/bin/ffmpeg
		test -x /usr/local/bin/ffprobe
		/usr/local/bin/ffmpeg -version >/dev/null
		/usr/local/bin/ffprobe -version >/dev/null
		/usr/local/bin/ffmpeg -hide_banner -encoders 2>/dev/null | grep -q "h264_nvenc"
		/usr/local/bin/ffmpeg -hide_banner -encoders 2>/dev/null | grep -q "hevc_nvenc"
	'
}

verify_distroless_image() {
	local image="$1"

	log "checking distroless image user"
	expect_image_user "${image}" "65532:65532"

	log "checking distroless entrypoint under default non-root user"
	docker run --rm "${image}" help >/dev/null
	docker run --rm "${image}" healthcheck --help >/dev/null
}

run_trivy_if_available() {
	local image="$1"

	if [[ "${USE_TRIVY}" != "1" ]]; then
		return 0
	fi
	if ! command -v trivy >/dev/null 2>&1; then
		log "trivy not installed; skipping local image scan for ${image}"
		return 0
	fi

	log "running trivy on ${image}"
	trivy image --severity CRITICAL,HIGH --ignore-unfixed=false --exit-code 1 "${image}"
}

main() {
	need_cmd docker
	need_cmd git

	cd "${ROOT}"

	log "building ffmpeg base image"
	docker_build "${BASE_TAG}" -f Dockerfile.ffmpeg-base .

	log "building main runtime image"
	docker_build "${RUNTIME_TAG}" --build-arg FFMPEG_BASE_IMAGE="${BASE_TAG}" -f Dockerfile .

	log "building distroless image"
	docker_build "${DISTROLESS_TAG}" -f Dockerfile.distroless backend

	verify_runtime_image "${RUNTIME_TAG}"
	verify_distroless_image "${DISTROLESS_TAG}"
	run_trivy_if_available "${RUNTIME_TAG}"
	run_trivy_if_available "${DISTROLESS_TAG}"

	log "image runtime verification complete"
}

main "$@"
