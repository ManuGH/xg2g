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
	hasVaapiDevice := false

	for i := range args {
		switch args[i] {
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

	container, packaging := classifyContainerArgs(segmentType, segmentFile)
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

// classifyContainerArgs maps the real HLS segment type to the container/packaging
// labels. It falls back to the segment filename extension when the segment type
// flag is implicit (.m4s -> fmp4, otherwise mpegts).
func classifyContainerArgs(segmentType, segmentFile string) (container, packaging string) {
	if strings.EqualFold(strings.TrimSpace(segmentType), "fmp4") ||
		strings.EqualFold(filepath.Ext(segmentFile), ".m4s") {
		return "fmp4", "fmp4"
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
