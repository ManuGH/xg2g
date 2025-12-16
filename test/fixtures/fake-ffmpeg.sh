#!/bin/sh
# Fake FFmpeg for testing
# Simulates ffmpeg behavior without actual transcoding
#
# Usage in tests:
#   export FFMPEG_BIN="$(pwd)/test/fixtures/fake-ffmpeg.sh"
#   export FAKE_FFPEG_EXIT=0  # or non-zero to simulate failure
#
# Environment variables:
#   FAKE_FFPEG_EXIT - Exit code to return (default: 0)
#   FAKE_FFMPEG_OUTPUT - Text to write to stderr (default: version info)

set -e

# Handle version check
if [ "$1" = "-version" ] || [ "$1" = "-v" ]; then
  echo "ffmpeg version 6.0-test (fake for testing)" >&2
  echo "configuration: --enable-fake-mode" >&2
  exit 0
fi

# Handle help
if [ "$1" = "-h" ] || [ "$1" = "-help" ] || [ "$1" = "--help" ]; then
  echo "usage: fake-ffmpeg [options] [[infile options] -i infile]... {[outfile options] outfile}..." >&2
  echo "This is a fake ffmpeg for testing purposes." >&2
  exit 0
fi

# Simulate normal operation
if [ -n "$FAKE_FFMPEG_OUTPUT" ]; then
  echo "$FAKE_FFMPEG_OUTPUT" >&2
fi

# Simulate some ffmpeg output
echo "fake-ffmpeg: Input #0, mpegts, from 'stream':" >&2
echo "fake-ffmpeg:   Duration: N/A, start: 0.000000, bitrate: N/A" >&2
echo "fake-ffmpeg:   Stream #0:0: Video: h264, 1920x1080" >&2
echo "fake-ffmpeg:   Stream #0:1: Audio: ac3, 48000 Hz, stereo" >&2
echo "fake-ffmpeg: Output #0, mpegts, to 'pipe:':" >&2
echo "fake-ffmpeg:   Stream #0:0: Video: copy" >&2
echo "fake-ffmpeg:   Stream #0:1: Audio: aac, 48000 Hz, stereo" >&2
echo "fake-ffmpeg: Stream mapping:" >&2
echo "fake-ffmpeg:   Stream #0:0 -> #0:0 (copy)" >&2
echo "fake-ffmpeg:   Stream #0:1 -> #0:1 (ac3 (native) -> aac (native))" >&2

# Exit with specified code (default: 0)
exit "${FAKE_FFPEG_EXIT:-0}"
