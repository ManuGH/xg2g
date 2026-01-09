#!/bin/bash
# Enterprise-Grade 2026: Mechanical Runtime Verification
# Verifies that the container image meets the canonical contract.
set -euo pipefail

IMAGE="${1:-xg2g:latest}"
echo "=== Auditing Image: ${IMAGE} ==="

# 1. Non-Root Check
echo -n "[Check 1] Non-root user... "
USER_ID=$(docker inspect --format='{{.Config.User}}' "${IMAGE}")
if [[ "${USER_ID}" == "10001:10001" ]]; then
    echo "PASS (User: ${USER_ID})"
else
    echo "FAIL (User: ${USER_ID:-root})"
    exit 1
fi

# 2. FFmpeg Contract Check
echo -n "[Check 2] Pinned FFmpeg version (7.1.3)... "
FFMPEG_VER=$(docker run --rm --entrypoint ffmpeg "${IMAGE}" -version | head -n1)
if [[ "${FFMPEG_VER}" == *"7.1.3"* ]]; then
    echo "PASS"
else
    echo "FAIL (Found: ${FFMPEG_VER})"
    exit 1
fi

# 3. Scoped LD_LIBRARY_PATH Check
echo -n "[Check 3] Scoped LD_LIBRARY_PATH... "
# The wrapper should set it for the process, we check if global ENV is clean
GLOBAL_LD=$(docker inspect --format='{{range .Config.Env}}{{println .}}{{end}}' "${IMAGE}" | grep LD_LIBRARY_PATH || true)
if [[ -z "${GLOBAL_LD}" ]]; then
    echo "PASS (Global env is clean)"
else
    echo "FAIL (Global leak detected: ${GLOBAL_LD})"
    exit 1
fi

# 4. Runtime Package Audit (Expected minimal SLIM)
echo -n "[Check 4] Runtime package audit... "
FOUND_TOOLS=()
for tool in git curl gcc make; do
    if docker run --rm --entrypoint sh "${IMAGE}" -c "command -v $tool" >/dev/null 2>&1; then
        FOUND_TOOLS+=("$tool")
    fi
done

if [ ${#FOUND_TOOLS[@]} -eq 0 ]; then
    echo "PASS (No build tools found)"
else
    echo "FAIL (Build tools present: ${FOUND_TOOLS[*]})"
    exit 1
fi

# 5. Permission Check (Sessions/Tmp)
echo -n "[Check 5] Directory permissions... "
docker run --rm --entrypoint sh "${IMAGE}" -c 'ls -ld /var/lib/xg2g' | grep -q "xg2g" && echo "PASS" || { echo "FAIL"; exit 1; }

# 6. ABI Audit (Shared Library Check)
echo -n "[Check 6] FFmpeg ABI audit (ldd check)... "
# Use the --print-realpath flag we just added to the wrapper
REAL_FFMPEG=$(docker run --rm --entrypoint ffmpeg "${IMAGE}" --print-realpath)

if [[ -z "${REAL_FFMPEG}" ]]; then
    echo "FAIL (ffmpeg --print-realpath returned empty)"
    exit 1
fi

# Run ldd on the real binary inside the container with robust LD_LIBRARY_PATH
LDD_OUT=$(docker run --rm --entrypoint sh "${IMAGE}" -c "LD_LIBRARY_PATH=/opt/ffmpeg/lib:/opt/ffmpeg/lib64 ldd ${REAL_FFMPEG}")

if echo "${LDD_OUT}" | grep -q "not found"; then
    echo "FAIL (Missing dependencies detected)"
    echo "${LDD_OUT}" | grep "not found"
    exit 1
fi

# Verify core libs resolve from /opt/ffmpeg (Enterprise Gate)
CORE_LIBS=(libavcodec libavformat libavutil libswscale libswresample)
for lib in "${CORE_LIBS[@]}"; do
    if ! echo "${LDD_OUT}" | grep -q "${lib}"; then
        echo "FAIL (Core library ${lib} missing from ldd output)"
        exit 1
    fi
done

CORE_LIBS_LEAK=$(echo "${LDD_OUT}" | grep -E 'lib(avcodec|avformat|avutil|swscale|swresample)' | grep -v '/opt/ffmpeg/' || true)
if [[ -n "${CORE_LIBS_LEAK}" ]]; then
    echo "FAIL (FFmpeg libs leaking from system paths!)"
    echo "${CORE_LIBS_LEAK}"
    exit 1
fi

echo "PASS (All 2026-pinned libs resolved from /opt/ffmpeg)"

echo "=== Audit Complete: SUCCESS ==="
