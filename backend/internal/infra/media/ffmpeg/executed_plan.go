// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ffmpeg

import (
	"path/filepath"
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

// executedFFmpegPlanFromArgs derives an execution-truth plan from the FINAL
// ffmpeg argv. It inspects only the flags ffmpeg was actually handed, so the
// result reflects what the process runs — never an upstream profile prediction.
// This is the single un-lie-able source for "what ffmpeg is doing": container,
// packaging, video/audio mode+codec, and hardware acceleration.
func executedFFmpegPlanFromArgs(args []string) ports.ExecutedFFmpegPlan {
	var segmentType, segmentFile, hwaccel, vcodec, acodec string
	var outFormat, movFlags string
	fragmented := false
	hasVaapiDevice := false

	for i := range args {
		switch args[i] {
		case "-f":
			if i+1 < len(args) {
				outFormat = args[i+1]
			}
		case "-i":
			// A -f seen before an -i described that INPUT's demuxer, not the
			// output muxer. Only the -f after the last input names the output.
			outFormat = ""
		case "-movflags":
			if i+1 < len(args) {
				movFlags = args[i+1]
			}
		case "-frag_duration":
			fragmented = true
		case "-hls_segment_type":
			if i+1 < len(args) {
				segmentType = args[i+1]
			}
		case "-hls_segment_filename":
			if i+1 < len(args) {
				segmentFile = args[i+1]
			}
		case "-hwaccel":
			if i+1 < len(args) {
				hwaccel = args[i+1]
			}
		case "-vaapi_device":
			hasVaapiDevice = true
		case "-c:v", "-codec:v", "-vcodec":
			if i+1 < len(args) {
				vcodec = args[i+1]
			}
		case "-c:a", "-codec:a", "-acodec":
			if i+1 < len(args) {
				acodec = args[i+1]
			}
		}
	}

	container, packaging := classifyContainerArgs(segmentType, segmentFile, outFormat, movFlags, fragmented)
	videoMode, videoCodec := classifyVideoCodecArg(vcodec)
	audioMode, audioCodec := classifyAudioCodecArg(acodec)

	return ports.ExecutedFFmpegPlan{
		Container:  container,
		Packaging:  packaging,
		HWAccel:    classifyHWAccelArgs(hwaccel, hasVaapiDevice, vcodec),
		VideoMode:  videoMode,
		VideoCodec: videoCodec,
		AudioMode:  audioMode,
		AudioCodec: audioCodec,
	}
}

// classifyContainerArgs maps the real output muxer flags to container/packaging
// labels. HLS-muxer runs are classified by segment type (or the .m4s filename
// when the type flag is implicit). The LL-HLS pipe mode never touches the hls
// muxer: ffmpeg writes one fragmented MP4 stream ("-f mp4" with frag movflags)
// to stdout and the in-process cmaf segmenter packages it into fMP4 segments —
// that argv must classify as fmp4, not fall back to mpegts (a wrong fallback
// here made the executed plan lie and fired a spurious plan_mismatch warning
// on every LL-HLS session). A plain, unfragmented "-f mp4" (VOD-style output)
// stays "mp4". Everything else is the mpegts stream path.
func classifyContainerArgs(segmentType, segmentFile, outFormat, movFlags string, fragmented bool) (container, packaging string) {
	if strings.EqualFold(strings.TrimSpace(segmentType), "fmp4") ||
		strings.EqualFold(filepath.Ext(segmentFile), ".m4s") {
		return "fmp4", "fmp4"
	}
	if strings.EqualFold(strings.TrimSpace(outFormat), "mp4") {
		flags := strings.ToLower(movFlags)
		if fragmented || strings.Contains(flags, "empty_moov") || strings.Contains(flags, "frag_") {
			return "fmp4", "fmp4"
		}
		return "mp4", "mp4"
	}
	return "mpegts", "ts"
}

// classifyVideoCodecArg maps the real -c:v value to (mode, normalized codec).
// An absent or "copy" encoder means the source video is passed through.
func classifyVideoCodecArg(c string) (mode, codec string) {
	switch strings.ToLower(strings.TrimSpace(c)) {
	case "", "copy":
		return "copy", "copy"
	case "libx264", "h264_vaapi", "h264_nvenc", "h264":
		return "transcode", "h264"
	case "libx265", "hevc_vaapi", "hevc_nvenc", "hevc":
		return "transcode", "hevc"
	case "libsvtav1", "av1_vaapi", "av1_nvenc", "av1":
		return "transcode", "av1"
	default:
		return "transcode", strings.ToLower(strings.TrimSpace(c))
	}
}

// classifyAudioCodecArg maps the real -c:a value to (mode, codec). Live output
// always sets an audio encoder; an absent value yields empty labels.
func classifyAudioCodecArg(c string) (mode, codec string) {
	switch v := strings.ToLower(strings.TrimSpace(c)); v {
	case "":
		return "", ""
	case "copy":
		return "copy", "copy"
	default:
		return "transcode", v
	}
}

// classifyHWAccelArgs distinguishes full VAAPI (decode+encode on GPU, marked by
// -hwaccel vaapi) from encode-only VAAPI (a *_vaapi encoder or -vaapi_device with
// software decode), and from NVENC. Pure copy/CPU paths report "none".
func classifyHWAccelArgs(hwaccel string, hasVaapiDevice bool, vcodec string) string {
	v := strings.ToLower(strings.TrimSpace(vcodec))
	switch {
	case strings.EqualFold(strings.TrimSpace(hwaccel), "vaapi"):
		return "vaapi"
	case strings.HasSuffix(v, "_vaapi") || hasVaapiDevice:
		return "vaapi_encode_only"
	case strings.HasSuffix(v, "_nvenc"):
		return "nvenc"
	default:
		return "none"
	}
}
