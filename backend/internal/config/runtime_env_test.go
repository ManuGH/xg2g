package config

import "testing"

func TestReadEnv_HLSProfilePathFallbacks(t *testing.T) {
	env, err := ReadEnv(func(key string) string {
		switch key {
		case "XG2G_WEB_FFMPEG_PATH":
			return " /custom/web-ffmpeg "
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("ReadEnv returned error: %v", err)
	}

	if got := env.Runtime.HLS.Safari.FFmpegPath; got != "/custom/web-ffmpeg" {
		t.Fatalf("Safari FFmpegPath = %q, want /custom/web-ffmpeg", got)
	}
	if got := env.Runtime.HLS.LLHLS.FFmpegPath; got != "/custom/web-ffmpeg" {
		t.Fatalf("LLHLS FFmpegPath = %q, want /custom/web-ffmpeg", got)
	}
}

func TestReadEnv_HLSProfileSpecificOverridesWin(t *testing.T) {
	env, err := ReadEnv(func(key string) string {
		switch key {
		case "XG2G_WEB_FFMPEG_PATH":
			return "/custom/web-ffmpeg"
		case "XG2G_SAFARI_DVR_FFMPEG_PATH":
			return "/custom/safari-ffmpeg"
		case "XG2G_LLHLS_FFMPEG_PATH":
			return "/custom/llhls-ffmpeg"
		case "XG2G_LLHLS_HEVC_BITRATE":
			return "7000k"
		case "XG2G_WEB_HEVC_BITRATE":
			return "6000k"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("ReadEnv returned error: %v", err)
	}

	if got := env.Runtime.HLS.Safari.FFmpegPath; got != "/custom/safari-ffmpeg" {
		t.Fatalf("Safari FFmpegPath = %q, want /custom/safari-ffmpeg", got)
	}
	if got := env.Runtime.HLS.LLHLS.FFmpegPath; got != "/custom/llhls-ffmpeg" {
		t.Fatalf("LLHLS FFmpegPath = %q, want /custom/llhls-ffmpeg", got)
	}
	if got := env.Runtime.HLS.LLHLS.HevcBitrate; got != "7000k" {
		t.Fatalf("LLHLS HevcBitrate = %q, want 7000k", got)
	}
}

func TestReadEnv_HLSOutputAndHevcFlags(t *testing.T) {
	env, err := ReadEnv(func(key string) string {
		switch key {
		case "XG2G_HLS_OUTPUT_DIR":
			return "   "
		case "XG2G_WEB_HEVC_PROFILE_ENABLED":
			return "true"
		case "XG2G_WEB_HEVC_MAXBITRATE":
			return "9000k"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("ReadEnv returned error: %v", err)
	}

	if got := env.Runtime.HLS.OutputDir; got != "" {
		t.Fatalf("HLS OutputDir = %q, want empty", got)
	}
	if !env.Runtime.HLS.LLHLS.HevcEnabled {
		t.Fatal("LLHLS HevcEnabled = false, want true")
	}
	if got := env.Runtime.HLS.LLHLS.HevcMaxBitrate; got != "9000k" {
		t.Fatalf("LLHLS HevcMaxBitrate = %q, want 9000k", got)
	}
}
