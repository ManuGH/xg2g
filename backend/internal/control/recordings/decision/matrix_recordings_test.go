package decision

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

func TestDecide_ClientMatrix(t *testing.T) {
	cases := []struct {
		name             string
		clientFixture    string
		sourceFixture    string
		requestedIntent  playbackprofile.PlaybackIntent
		supportsRange    *bool
		wantMode         Mode
		wantRequested    string
		wantResolved     string
		wantRung         string
		wantAudioRung    string
		wantVideoRung    string
		wantDegradedFrom string
		wantContainer    string
		wantPackaging    playbackprofile.Packaging
		wantVideoMode    playbackprofile.MediaMode
		wantVideoCodec   string
		wantVideoCRF     int
		wantVideoPreset  string
		wantAudioMode    playbackprofile.MediaMode
		wantAudioCodec   string
		wantAudioBitrate int
		wantAudioCh      int
	}{
		{
			name:            "safari native direct play h264 aac",
			clientFixture:   playbackprofile.ClientSafariNative,
			sourceFixture:   playbackprofile.SourceH264AAC,
			requestedIntent: playbackprofile.IntentDirect,
			wantMode:        ModeDirectPlay,
			wantRequested:   "direct",
			wantResolved:    "direct",
			wantRung:        "direct_copy",
			wantContainer:   "mp4",
			wantPackaging:   playbackprofile.PackagingMP4,
			wantVideoMode:   playbackprofile.MediaModeCopy,
			wantVideoCodec:  "h264",
			wantAudioMode:   playbackprofile.MediaModeCopy,
			wantAudioCodec:  "aac",
		},
		{
			name:            "ios safari stays direct for h264 ac3",
			clientFixture:   playbackprofile.ClientIOSSafariNative,
			sourceFixture:   playbackprofile.SourceH264AC3,
			requestedIntent: playbackprofile.IntentCompatible,
			wantMode:        ModeDirectPlay,
			wantRequested:   "compatible",
			wantResolved:    "direct",
			wantRung:        "direct_copy",
			wantContainer:   "mpegts",
			wantPackaging:   playbackprofile.PackagingTS,
			wantVideoMode:   playbackprofile.MediaModeCopy,
			wantVideoCodec:  "h264",
			wantAudioMode:   playbackprofile.MediaModeCopy,
			wantAudioCodec:  "ac3",
		},
		{
			name:            "firefox hlsjs falls to direct stream without range",
			clientFixture:   playbackprofile.ClientFirefoxHLSJS,
			sourceFixture:   playbackprofile.SourceH264AAC,
			requestedIntent: playbackprofile.IntentCompatible,
			supportsRange:   boolPtrTest(false),
			wantMode:        ModeDirectStream,
			wantRequested:   "compatible",
			wantResolved:    "compatible",
			wantRung:        "compatible_hls_ts",
			wantContainer:   "mpegts",
			wantPackaging:   playbackprofile.PackagingTS,
			wantVideoMode:   playbackprofile.MediaModeCopy,
			wantVideoCodec:  "h264",
			wantAudioMode:   playbackprofile.MediaModeCopy,
			wantAudioCodec:  "aac",
		},
		{
			name:             "firefox hlsjs transcodes unsupported ac3 with compatible ladder",
			clientFixture:    playbackprofile.ClientFirefoxHLSJS,
			sourceFixture:    playbackprofile.SourceH264AC3,
			requestedIntent:  playbackprofile.IntentCompatible,
			wantMode:         ModeTranscode,
			wantRequested:    "compatible",
			wantResolved:     "compatible",
			wantRung:         "compatible_audio_aac_256_stereo",
			wantAudioRung:    "compatible_audio_aac_256_stereo",
			wantContainer:    "mpegts",
			wantPackaging:    playbackprofile.PackagingTS,
			wantVideoMode:    playbackprofile.MediaModeCopy,
			wantVideoCodec:   "h264",
			wantAudioMode:    playbackprofile.MediaModeTranscode,
			wantAudioCodec:   "aac",
			wantAudioBitrate: 256,
			wantAudioCh:      2,
		},
		{
			name:             "firefox hlsjs quality raises aac bitrate",
			clientFixture:    playbackprofile.ClientFirefoxHLSJS,
			sourceFixture:    playbackprofile.SourceH264AC3,
			requestedIntent:  playbackprofile.IntentQuality,
			wantMode:         ModeTranscode,
			wantRequested:    "quality",
			wantResolved:     "quality",
			wantRung:         "quality_audio_aac_320_stereo",
			wantAudioRung:    "quality_audio_aac_320_stereo",
			wantContainer:    "mpegts",
			wantPackaging:    playbackprofile.PackagingTS,
			wantVideoMode:    playbackprofile.MediaModeCopy,
			wantVideoCodec:   "h264",
			wantAudioMode:    playbackprofile.MediaModeTranscode,
			wantAudioCodec:   "aac",
			wantAudioBitrate: 320,
			wantAudioCh:      2,
		},
		{
			name:             "firefox hlsjs repair lowers aac bitrate",
			clientFixture:    playbackprofile.ClientFirefoxHLSJS,
			sourceFixture:    playbackprofile.SourceH264AC3,
			requestedIntent:  playbackprofile.IntentRepair,
			wantMode:         ModeTranscode,
			wantRequested:    "repair",
			wantResolved:     "repair",
			wantRung:         "repair_audio_aac_192_stereo",
			wantAudioRung:    "repair_audio_aac_192_stereo",
			wantContainer:    "mpegts",
			wantPackaging:    playbackprofile.PackagingTS,
			wantVideoMode:    playbackprofile.MediaModeCopy,
			wantVideoCodec:   "h264",
			wantAudioMode:    playbackprofile.MediaModeTranscode,
			wantAudioCodec:   "aac",
			wantAudioBitrate: 192,
			wantAudioCh:      2,
		},
		{
			name:             "firefox hlsjs direct degrades to compatible when audio transcode is needed",
			clientFixture:    playbackprofile.ClientFirefoxHLSJS,
			sourceFixture:    playbackprofile.SourceH264AC3,
			requestedIntent:  playbackprofile.IntentDirect,
			wantMode:         ModeTranscode,
			wantRequested:    "direct",
			wantResolved:     "compatible",
			wantRung:         "compatible_audio_aac_256_stereo",
			wantAudioRung:    "compatible_audio_aac_256_stereo",
			wantDegradedFrom: "direct",
			wantContainer:    "mpegts",
			wantPackaging:    playbackprofile.PackagingTS,
			wantVideoMode:    playbackprofile.MediaModeCopy,
			wantVideoCodec:   "h264",
			wantAudioMode:    playbackprofile.MediaModeTranscode,
			wantAudioCodec:   "aac",
			wantAudioBitrate: 256,
			wantAudioCh:      2,
		},
		{
			name:            "chromium hlsjs transcodes unsupported hevc video only",
			clientFixture:   playbackprofile.ClientChromiumHLSJS,
			sourceFixture:   playbackprofile.SourceHEVCAAC,
			requestedIntent: playbackprofile.IntentCompatible,
			wantMode:        ModeTranscode,
			wantRequested:   "compatible",
			wantResolved:    "compatible",
			wantRung:        "compatible_video_h264_crf23_fast",
			wantVideoRung:   "compatible_video_h264_crf23_fast",
			wantContainer:   "mpegts",
			wantPackaging:   playbackprofile.PackagingTS,
			wantVideoMode:   playbackprofile.MediaModeTranscode,
			wantVideoCodec:  "h264",
			wantVideoCRF:    23,
			wantVideoPreset: "fast",
			wantAudioMode:   playbackprofile.MediaModeCopy,
			wantAudioCodec:  "aac",
		},
		{
			name:            "chromium hlsjs keeps dirty dvb playable",
			clientFixture:   playbackprofile.ClientChromiumHLSJS,
			sourceFixture:   playbackprofile.SourceDVBDirty,
			requestedIntent: playbackprofile.IntentCompatible,
			wantMode:        ModeDirectPlay,
			wantRequested:   "compatible",
			wantResolved:    "direct",
			wantRung:        "direct_copy",
			wantContainer:   "mpegts",
			wantPackaging:   playbackprofile.PackagingTS,
			wantVideoMode:   playbackprofile.MediaModeCopy,
			wantVideoCodec:  "h264",
			wantAudioMode:   playbackprofile.MediaModeCopy,
			wantAudioCodec:  "aac",
		},
		{
			name:            "chromium hlsjs keeps interlaced mpegts as direct play under current model",
			clientFixture:   playbackprofile.ClientChromiumHLSJS,
			sourceFixture:   playbackprofile.SourceMPEGTSInterlaced,
			requestedIntent: playbackprofile.IntentCompatible,
			wantMode:        ModeDirectPlay,
			wantRequested:   "compatible",
			wantResolved:    "direct",
			wantRung:        "direct_copy",
			wantContainer:   "mpegts",
			wantPackaging:   playbackprofile.PackagingTS,
			wantVideoMode:   playbackprofile.MediaModeCopy,
			wantVideoCodec:  "h264",
			wantAudioMode:   playbackprofile.MediaModeCopy,
			wantAudioCodec:  "aac",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := DecisionInput{
				Source:          sourceFromFixture(playbackprofile.MustSourceFixture(tc.sourceFixture)),
				Capabilities:    capabilitiesFromFixture(playbackprofile.MustClientFixture(tc.clientFixture), tc.supportsRange),
				Policy:          Policy{AllowTranscode: true},
				RequestedIntent: tc.requestedIntent,
				APIVersion:      "v3",
				RequestID:       "matrix-test",
			}

			_, dec, prob := Decide(t.Context(), input, "matrix")
			if prob != nil || dec == nil {
				t.Fatalf("expected decision, got problem=%v", prob)
			}

			if dec.Mode != tc.wantMode {
				t.Fatalf("unexpected mode: got=%s want=%s", dec.Mode, tc.wantMode)
			}
			if dec.Trace.RequestedIntent != tc.wantRequested {
				t.Fatalf("unexpected requested intent: got=%q want=%q", dec.Trace.RequestedIntent, tc.wantRequested)
			}
			if dec.Trace.ResolvedIntent != tc.wantResolved {
				t.Fatalf("unexpected resolved intent: got=%q want=%q", dec.Trace.ResolvedIntent, tc.wantResolved)
			}
			if dec.Trace.QualityRung != tc.wantRung {
				t.Fatalf("unexpected quality rung: got=%q want=%q", dec.Trace.QualityRung, tc.wantRung)
			}
			if dec.Trace.AudioQualityRung != tc.wantAudioRung {
				t.Fatalf("unexpected audio quality rung: got=%q want=%q", dec.Trace.AudioQualityRung, tc.wantAudioRung)
			}
			if dec.Trace.VideoQualityRung != tc.wantVideoRung {
				t.Fatalf("unexpected video quality rung: got=%q want=%q", dec.Trace.VideoQualityRung, tc.wantVideoRung)
			}
			if dec.Trace.DegradedFrom != tc.wantDegradedFrom {
				t.Fatalf("unexpected degradedFrom: got=%q want=%q", dec.Trace.DegradedFrom, tc.wantDegradedFrom)
			}

			if dec.TargetProfile == nil {
				t.Fatal("expected target profile")
			}
			if dec.TargetProfile.Container != tc.wantContainer {
				t.Fatalf("unexpected target container: got=%q want=%q", dec.TargetProfile.Container, tc.wantContainer)
			}
			if dec.TargetProfile.Packaging != tc.wantPackaging {
				t.Fatalf("unexpected target packaging: got=%q want=%q", dec.TargetProfile.Packaging, tc.wantPackaging)
			}
			if dec.TargetProfile.Video.Mode != tc.wantVideoMode || dec.TargetProfile.Video.Codec != tc.wantVideoCodec {
				t.Fatalf("unexpected video target: got=%#v", dec.TargetProfile.Video)
			}
			if tc.wantVideoCRF != 0 && dec.TargetProfile.Video.CRF != tc.wantVideoCRF {
				t.Fatalf("unexpected video crf: got=%d want=%d", dec.TargetProfile.Video.CRF, tc.wantVideoCRF)
			}
			if tc.wantVideoPreset != "" && dec.TargetProfile.Video.Preset != tc.wantVideoPreset {
				t.Fatalf("unexpected video preset: got=%q want=%q", dec.TargetProfile.Video.Preset, tc.wantVideoPreset)
			}
			if dec.TargetProfile.Audio.Mode != tc.wantAudioMode || dec.TargetProfile.Audio.Codec != tc.wantAudioCodec {
				t.Fatalf("unexpected audio target: got=%#v", dec.TargetProfile.Audio)
			}
			if tc.wantAudioBitrate != 0 && dec.TargetProfile.Audio.BitrateKbps != tc.wantAudioBitrate {
				t.Fatalf("unexpected audio bitrate: got=%d want=%d", dec.TargetProfile.Audio.BitrateKbps, tc.wantAudioBitrate)
			}
			if tc.wantAudioCh != 0 && dec.TargetProfile.Audio.Channels != tc.wantAudioCh {
				t.Fatalf("unexpected audio channels: got=%d want=%d", dec.TargetProfile.Audio.Channels, tc.wantAudioCh)
			}
		})
	}
}

func sourceFromFixture(src playbackprofile.SourceProfile) Source {
	return Source{
		Container:   src.Container,
		VideoCodec:  src.VideoCodec,
		AudioCodec:  src.AudioCodec,
		BitrateKbps: src.BitrateKbps,
		Width:       src.Width,
		Height:      src.Height,
		FPS:         src.FPS,
	}
}

func capabilitiesFromFixture(client playbackprofile.ClientPlaybackProfile, supportsRangeOverride *bool) Capabilities {
	supportsRange := client.SupportsRange
	if supportsRangeOverride != nil {
		supportsRange = *supportsRangeOverride
	}

	var maxVideo *MaxVideoDimensions
	if client.MaxVideo != nil {
		maxVideo = &MaxVideoDimensions{
			Width:  client.MaxVideo.Width,
			Height: client.MaxVideo.Height,
		}
	}

	return Capabilities{
		Version:       2,
		Containers:    append([]string(nil), client.Containers...),
		VideoCodecs:   append([]string(nil), client.VideoCodecs...),
		AudioCodecs:   append([]string(nil), client.AudioCodecs...),
		SupportsHLS:   client.SupportsHLS,
		SupportsRange: boolPtrTest(supportsRange),
		MaxVideo:      maxVideo,
		DeviceType:    client.DeviceType,
	}
}

func boolPtrTest(v bool) *bool {
	return &v
}
