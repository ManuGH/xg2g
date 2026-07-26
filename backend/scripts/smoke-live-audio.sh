#!/usr/bin/env bash
# Replay a real session's ffmpeg argument vector against a synthetic DVB-like
# fixture and assert the audio contract of the produced HLS output.
#
# Why this exists: on 2026-07-26 three "fixes" were deployed that changed nothing
# (or broke playback outright) because the only available oracle was a human
# zapping the receiver and listening. Unit tests stayed green while production
# shipped 192k, and ffmpeg's own rejection ("Same elementary stream found more than
# once" -> exit 234) is invisible to any Go test. This script closes both gaps
# without touching the tuner, and without depending on the receiver's condition.
#
# It deliberately takes the argument vector from the daemon instead of rebuilding
# it, so it can never drift from what production actually runs. The daemon logs it
# under startup_phase=ffmpeg_args_built (credentials already redacted).
#
# Usage:
#   smoke-live-audio.sh [--fixture PATH] [--expect-audio-bitrate-k N]
#                       [--expect-audio-renditions N] [--keep] -- <ffmpeg args...>
#
# Extract the vector from a running staging instance (needs python3 on the caller,
# not in the container):
#   docker logs xg2g-staging --since 10m 2>&1 \
#     | grep ffmpeg_args_built | tail -1 \
#     | python3 -c 'import json,sys; print(" ".join(json.loads(sys.stdin.read())["args"]))'
#
# Requires: ffmpeg, ffprobe.

set -euo pipefail

FIXTURE=""
EXPECT_AUDIO_KBPS=0
EXPECT_RENDITIONS=2
KEEP=0

die() {
	echo "❌ $*" >&2
	exit 1
}
note() { echo "   $*"; }

while [[ $# -gt 0 ]]; do
	case "$1" in
	--fixture)
		FIXTURE="$2"
		shift 2
		;;
	--expect-audio-bitrate-k)
		EXPECT_AUDIO_KBPS="$2"
		shift 2
		;;
	--expect-audio-renditions)
		EXPECT_RENDITIONS="$2"
		shift 2
		;;
	--keep)
		KEEP=1
		shift
		;;
	--)
		shift
		break
		;;
	*) die "unknown option: $1" ;;
	esac
done

[[ $# -gt 0 ]] || die "no ffmpeg argument vector given (pass it after --)"
command -v ffmpeg >/dev/null 2>&1 || die "ffmpeg not found"
command -v ffprobe >/dev/null 2>&1 || die "ffprobe not found"

WORK="$(mktemp -d)"
cleanup() { [[ "${KEEP}" == "1" ]] || rm -rf "${WORK}"; }
trap cleanup EXIT
[[ "${KEEP}" == "1" ]] && note "workdir kept: ${WORK}"

# ---------------------------------------------------------------------------
# 1. Fixture: mpegts carrying interlaced-flagged H.264 plus the two AC-3 tracks
#    the German DVB muxes ship (5.1 primary + stereo secondary). Synthetic on
#    purpose — a real capture also carries the receiver's corruption phases,
#    which makes a regression oracle non-deterministic.
# ---------------------------------------------------------------------------
if [[ -z "${FIXTURE}" ]]; then
	FIXTURE="${WORK}/dvb-fixture.ts"
	note "building fixture ${FIXTURE}"
	# White noise, not a sine: a tone is trivially compressible, so the AAC encoder
	# stays far below its target bitrate and a "measured ~= expected" assertion
	# cannot tell 192k from 320k. Noise saturates the encoder budget.
	ffmpeg -hide_banner -loglevel error -y \
		-f lavfi -i "testsrc2=size=1920x1080:rate=25:duration=8" \
		-f lavfi -i "anoisesrc=duration=8:sample_rate=48000:amplitude=0.5" \
		-f lavfi -i "anoisesrc=duration=8:sample_rate=48000:amplitude=0.4:seed=42" \
		-filter_complex "[1:a]pan=5.1(side)|FL=c0|FR=c0|FC=c0|LFE=c0|SL=c0|SR=c0[a51]" \
		-map 0:v -map "[a51]" -map 2:a \
		-c:v libx264 -preset ultrafast -g 50 -pix_fmt yuv420p \
		-c:a:0 ac3 -b:a:0 384k -metadata:s:a:0 language=deu \
		-c:a:1 ac3 -b:a:1 192k -metadata:s:a:1 language=eng \
		-f mpegts "${FIXTURE}" || die "fixture build failed"
fi
[[ -s "${FIXTURE}" ]] || die "fixture is empty: ${FIXTURE}"

# ---------------------------------------------------------------------------
# 2. Rewrite the production vector for offline replay:
#    - output paths move into the workdir (they point at the session directory)
#    - -ss before the input is dropped: the orphan correction seeks to a live
#      timeline position (e.g. 54205s) that does not exist in a fixture
#    - -headers is dropped (its value is redacted in the log anyway)
#    - a pipe:0 input is fed from the fixture on stdin, a URL input is replaced
# ---------------------------------------------------------------------------
declare -a ARGS=()
declare -a DROPPED=()
USES_STDIN=0
seen_input=0
skip_next=0
map_flag=0
audio_map_seq=0
for arg in "$@"; do
	if [[ "${skip_next}" == "1" ]]; then
		skip_next=0
		continue
	fi
	# Production maps absolute DVB stream indices (-map 0:3? -map 0:4?) which do
	# not exist in the fixture; the trailing '?' would swallow them silently and
	# the run would "pass" with fewer audio tracks. Rewrite positionally instead:
	# the first -map is the video stream, every following one is the next audio
	# stream. That preserves the number of mapped audio outputs, which is what the
	# audio contract is about.
	if [[ "${map_flag}" == "1" ]]; then
		map_flag=0
		case "${arg}" in
		0:v*) ARGS+=("0:v:0?") ;;
		0:a*) ARGS+=("${arg}") ;;
		0:[0-9]*)
			ARGS+=("0:a:${audio_map_seq}?")
			audio_map_seq=$((audio_map_seq + 1))
			;;
		*) ARGS+=("${arg}") ;;
		esac
		continue
	fi
	if [[ "${arg}" == "-map" ]]; then
		map_flag=1
		ARGS+=("${arg}")
		continue
	fi
	case "${arg}" in
	-headers)
		DROPPED+=("-headers <value>")
		skip_next=1
		continue
		;;
	-ss)
		if [[ "${seen_input}" == "0" ]]; then
			DROPPED+=("-ss <input seek>")
			skip_next=1
			continue
		fi
		;;
	pipe:0)
		USES_STDIN=1
		seen_input=1
		;;
	*://*)
		if [[ "${seen_input}" == "0" ]]; then
			arg="${FIXTURE}"
			seen_input=1
		fi
		;;
	/var/lib/xg2g/hls/sessions/*)
		arg="${WORK}/$(basename "${arg}")"
		;;
	/dev/shm/xg2g/sessions/*)
		arg="${WORK}/$(basename "${arg}")"
		;;
	esac
	ARGS+=("${arg}")
done

((${#DROPPED[@]})) && note "dropped for replay: ${DROPPED[*]}"

echo "▶ replaying ${#ARGS[@]} args against fixture"
if [[ "${USES_STDIN}" == "1" ]]; then
	ffmpeg -hide_banner -loglevel warning -y "${ARGS[@]}" <"${FIXTURE}" >"${WORK}/ffmpeg.log" 2>&1 && rc=0 || rc=$?
else
	ffmpeg -hide_banner -loglevel warning -y "${ARGS[@]}" >"${WORK}/ffmpeg.log" 2>&1 && rc=0 || rc=$?
fi

if [[ "${rc}" != "0" ]]; then
	echo "❌ ffmpeg rejected the production argument vector (exit ${rc})" >&2
	tail -20 "${WORK}/ffmpeg.log" >&2
	exit 1
fi
note "ffmpeg exit 0"

# ---------------------------------------------------------------------------
# 3. Assert the audio contract on the actual output.
# ---------------------------------------------------------------------------
FAILED=0
fail() {
	echo "❌ $*" >&2
	FAILED=1
}

MASTER=""
for candidate in "${WORK}/index.m3u8" "${WORK}/master.m3u8"; do
	[[ -f "${candidate}" ]] && MASTER="${candidate}"
done

if [[ -n "${MASTER}" ]]; then
	renditions=$(grep -c 'TYPE=AUDIO' "${MASTER}" || true)
	note "master playlist: ${renditions} audio rendition(s)"
	[[ "${renditions}" == "${EXPECT_RENDITIONS}" ]] ||
		fail "expected ${EXPECT_RENDITIONS} audio renditions in master playlist, found ${renditions}"
	grep -q 'DEFAULT=YES' "${MASTER}" || fail "no DEFAULT=YES audio rendition — players start silent"
elif [[ "${EXPECT_RENDITIONS}" != "0" ]]; then
	fail "no master playlist written, so no audio renditions are advertised"
fi

# Zero-byte segments mean the muxer produced a playlist entry with no media.
while IFS= read -r seg; do
	[[ -s "${seg}" ]] || fail "zero-byte segment: $(basename "${seg}")"
done < <(find "${WORK}" -name 'seg_*' -o -name '*.m4s' -o -name '*.ts' 2>/dev/null | grep -v dvb-fixture || true)

audio_streams=0
while IFS= read -r init; do
	# A failing read must not abort the run under `set -e`; a missing audio stream
	# in an init segment is a finding to report, not a reason to die silently.
	codec=""
	channels=""
	read -r codec channels < <(ffprobe -v error -select_streams a \
		-show_entries stream=codec_name,channels -of csv=p=0:nk=1 "${init}" 2>/dev/null | head -1 | tr ',' ' ') || true
	[[ -z "${codec:-}" ]] && continue
	audio_streams=$((audio_streams + 1))

	# Measured, not declared: bytes over duration of the finished segments.
	base="$(basename "${init}")"
	idx="${base#init_}"
	idx="${idx%.mp4}"
	bytes=0
	count=0
	for seg in "${WORK}"/seg_"${idx}"_*; do
		[[ -f "${seg}" ]] || continue
		bytes=$((bytes + $(wc -c <"${seg}")))
		count=$((count + 1))
	done
	if ((count > 0)); then
		# Duration comes from the playlist, not from ffprobe: a bare fMP4 segment
		# has no moov of its own, so probing it yields N/A and the measurement
		# would silently degrade to "unknown".
		measured=""
		playlist="${WORK}/stream_${idx}.m3u8"
		if [[ -f "${playlist}" ]]; then
			measured=$(awk -F: -v b="${bytes}" '
				/^#EXTINF:/ { split($2, f, ","); d += f[1] }
				END { if (d > 0) printf "%d", (b * 8) / d / 1000 }' "${playlist}")
		fi
		note "audio rendition ${idx}: ${codec} ${channels}ch, ${count} segment(s), ~${measured:-unmeasurable} kbit/s measured"
		if [[ "${EXPECT_AUDIO_KBPS}" != "0" ]]; then
			if [[ -z "${measured}" ]]; then
				# Never let a failed measurement read as a pass.
				fail "rendition ${idx}: could not measure bitrate (no playlist durations), expected ~${EXPECT_AUDIO_KBPS} kbit/s"
			else
				lo=$((EXPECT_AUDIO_KBPS * 75 / 100))
				hi=$((EXPECT_AUDIO_KBPS * 125 / 100))
				((measured >= lo && measured <= hi)) ||
					fail "rendition ${idx} measured ${measured} kbit/s, expected ~${EXPECT_AUDIO_KBPS} kbit/s (window ${lo}-${hi})"
			fi
		fi
	else
		note "audio rendition ${idx}: ${codec} ${channels}ch (no finished segments)"
		[[ "${EXPECT_AUDIO_KBPS}" == "0" ]] ||
			fail "rendition ${idx} produced no finished segments, so nothing could be measured"
	fi
done < <(find "${WORK}" -name 'init_*.mp4' | sort)

if [[ "${EXPECT_RENDITIONS}" != "0" ]]; then
	((audio_streams >= EXPECT_RENDITIONS)) ||
		fail "found ${audio_streams} audio init segment(s), expected ${EXPECT_RENDITIONS}"
fi

if grep -qE 'Same elementary stream found more than once|Could not write header' "${WORK}/ffmpeg.log"; then
	fail "muxer rejected the stream mapping (see ffmpeg.log)"
fi

if [[ "${FAILED}" != "0" ]]; then
	echo "❌ audio smoke FAILED" >&2
	exit 1
fi
echo "✅ audio smoke passed"
