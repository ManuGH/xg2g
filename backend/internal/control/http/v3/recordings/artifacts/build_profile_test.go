package artifacts

import "testing"

func TestRecordingTargetProfile_DefaultWebPlayback(t *testing.T) {
	target := recordingTargetProfile("generic")
	if target == nil {
		t.Fatal("expected target profile")
	}
	if target.Video.Mode != "transcode" || target.Video.Codec != "h264" || target.Video.CRF != 23 || target.Video.Preset != "fast" {
		t.Fatalf("expected default profile to use compatible video ladder, got %#v", target.Video)
	}
	if target.Audio.Mode != "transcode" || target.Audio.Codec != "aac" || target.Audio.BitrateKbps != 256 {
		t.Fatalf("unexpected default audio target: %#v", target.Audio)
	}
	if !target.HLS.Enabled || target.HLS.SegmentContainer != "mpegts" || target.Packaging != "ts" {
		t.Fatalf("unexpected default hls target: %#v", target.HLS)
	}
}

func TestRecordingTargetProfile_SafariUsesCompatibleFMP4Packaging(t *testing.T) {
	target := recordingTargetProfile("safari")
	if target == nil {
		t.Fatal("expected target profile")
	}
	if target.Video.Mode != "transcode" || target.Video.Codec != "h264" || target.Video.CRF != 23 || target.Video.Preset != "fast" {
		t.Fatalf("expected safari to use compatible video ladder, got %#v", target.Video)
	}
	if target.Audio.Mode != "transcode" || target.Audio.Codec != "aac" || target.Audio.BitrateKbps != 256 {
		t.Fatalf("unexpected safari audio target: %#v", target.Audio)
	}
	if target.Packaging != "fmp4" || target.HLS.SegmentContainer != "fmp4" || target.Container != "mp4" {
		t.Fatalf("expected safari to use fmp4 packaging, got %#v", target)
	}
}

func TestRecordingTargetProfile_AndroidNativeUsesCompatibleFMP4Packaging(t *testing.T) {
	target := recordingTargetProfile("android_native")
	if target == nil {
		t.Fatal("expected target profile")
	}
	if target.Video.Mode != "transcode" || target.Video.Codec != "h264" || target.Video.CRF != 23 || target.Video.Preset != "fast" {
		t.Fatalf("expected android_native to use compatible video ladder, got %#v", target.Video)
	}
	if target.Audio.Mode != "transcode" || target.Audio.Codec != "aac" || target.Audio.BitrateKbps != 256 {
		t.Fatalf("unexpected android_native audio target: %#v", target.Audio)
	}
	if target.Packaging != "fmp4" || target.HLS.SegmentContainer != "fmp4" || target.Container != "mp4" {
		t.Fatalf("expected android_native to use fmp4 packaging, got %#v", target)
	}
}

func TestRecordingTargetProfile_QualityUsesHigherAACBitrate(t *testing.T) {
	target := recordingTargetProfile("quality")
	if target == nil {
		t.Fatal("expected target profile")
	}
	if target.Video.Mode != "transcode" || target.Video.Codec != "h264" || target.Video.CRF != 20 || target.Video.Preset != "slow" {
		t.Fatalf("expected quality to use higher-quality video ladder, got %#v", target.Video)
	}
	if target.Audio.BitrateKbps != 320 {
		t.Fatalf("expected quality audio bitrate 320, got %#v", target.Audio)
	}
	if target.Packaging != "ts" || target.HLS.SegmentContainer != "mpegts" {
		t.Fatalf("expected quality to keep ts packaging, got %#v", target)
	}
}

func TestRecordingTargetProfile_RepairUsesConservativeH264AAC(t *testing.T) {
	target := recordingTargetProfile("repair")
	if target == nil {
		t.Fatal("expected target profile")
	}
	if target.Video.Mode != "transcode" || target.Video.Codec != "h264" || target.Video.CRF != 28 || target.Video.Preset != "veryfast" {
		t.Fatalf("expected repair to transcode video to h264, got %#v", target.Video)
	}
	if target.Audio.Mode != "transcode" || target.Audio.Codec != "aac" || target.Audio.BitrateKbps != 192 {
		t.Fatalf("unexpected repair audio target: %#v", target.Audio)
	}
}

func TestRecordingTargetProfile_SafariDirtyUsesRepairFMP4Packaging(t *testing.T) {
	target := recordingTargetProfile("safari_dirty")
	if target == nil {
		t.Fatal("expected target profile")
	}
	if target.Video.Mode != "transcode" || target.Video.Codec != "h264" || target.Video.CRF != 28 || target.Video.Preset != "veryfast" {
		t.Fatalf("expected safari_dirty to transcode video to h264, got %#v", target.Video)
	}
	if target.Audio.BitrateKbps != 192 {
		t.Fatalf("expected safari_dirty repair audio bitrate, got %#v", target.Audio)
	}
	if target.Packaging != "fmp4" || target.HLS.SegmentContainer != "fmp4" {
		t.Fatalf("expected safari_dirty to use fmp4 packaging, got %#v", target)
	}
}

func TestRecordingTargetProfile_DirectKeepsCopyVideoAndAudio(t *testing.T) {
	target := recordingTargetProfile("direct")
	if target == nil {
		t.Fatal("expected target profile")
	}
	if target.Video.Mode != "copy" {
		t.Fatalf("expected direct to keep copy-video, got %#v", target.Video)
	}
	if target.Audio.Mode != "copy" {
		t.Fatalf("expected direct to keep copy-audio, got %#v", target.Audio)
	}
}
