package recordings

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

func TestResolveLiveNativeTransportPlan_DirectPlayTSPrefersDirectStreamFMP4(t *testing.T) {
	plan := resolvePlaybackTransportPlan(
		PlaybackInfoRequest{SubjectKind: PlaybackSubjectLive},
		capabilities.PlaybackCapabilities{
			ClientFamilyFallback: "ios_safari_native",
		},
		&decision.Decision{
			Mode: decision.ModeDirectPlay,
			TargetProfile: &playbackprofile.TargetPlaybackProfile{
				Container: "mpegts",
				Packaging: playbackprofile.PackagingTS,
				Video: playbackprofile.VideoTarget{
					Mode:  playbackprofile.MediaModeCopy,
					Codec: "h264",
				},
				Audio: playbackprofile.AudioTarget{
					Mode:  playbackprofile.MediaModeCopy,
					Codec: "aac",
				},
			},
		},
	)

	if !plan.applied {
		t.Fatal("expected live native transport plan")
	}
	if plan.id != playbackTransportPlanLiveNativeDirectStream {
		t.Fatalf("unexpected plan id: got %q", plan.id)
	}
	if !plan.rewriteDirectStream {
		t.Fatal("expected live plan to rewrite direct play to direct stream")
	}
	if plan.targetContainer != "fmp4" || plan.targetPackaging != playbackprofile.PackagingFMP4 || plan.hlsSegmentContainer != "fmp4" {
		t.Fatalf("unexpected live plan packaging: %#v", plan)
	}
	if plan.selectedContainer != "fmp4" {
		t.Fatalf("expected selected container rewrite to fmp4, got %#v", plan)
	}
}

func TestResolveRecordingNativeTransportPlan_AndroidTVDirectPlayTSKeepsExistingTransport(t *testing.T) {
	plan := resolvePlaybackTransportPlan(
		PlaybackInfoRequest{SubjectKind: PlaybackSubjectRecording},
		capabilities.PlaybackCapabilities{
			ClientFamilyFallback: "android_tv_native",
		},
		&decision.Decision{
			Mode: decision.ModeDirectPlay,
			TargetProfile: &playbackprofile.TargetPlaybackProfile{
				Container: "mpegts",
				Packaging: playbackprofile.PackagingTS,
				Video: playbackprofile.VideoTarget{
					Mode:  playbackprofile.MediaModeCopy,
					Codec: "h264",
				},
				Audio: playbackprofile.AudioTarget{
					Mode:  playbackprofile.MediaModeCopy,
					Codec: "ac3",
				},
			},
		},
	)

	if plan.applied {
		t.Fatalf("expected no transport rewrite plan for android native direct TS, got %#v", plan)
	}
}

func TestResolveLiveNativeTransportPlan_ExperimentalDesktopSafariAV1TSBypassesFMP4Rewrite(t *testing.T) {
	t.Setenv("XG2G_EXPERIMENTAL_AV1_MPEGTS_ENABLED", "true")

	plan := resolvePlaybackTransportPlan(
		PlaybackInfoRequest{SubjectKind: PlaybackSubjectLive},
		capabilities.PlaybackCapabilities{
			ClientFamilyFallback: playbackprofile.ClientSafariNative,
			ClientCapsSource:     capabilities.ClientCapsSourceRuntimePlusFam,
			VideoCodecs:          []string{"av1"},
			Containers:           []string{"mpegts", "hls"},
		},
		&decision.Decision{
			Mode: decision.ModeTranscode,
			Selected: decision.SelectedFormats{
				VideoCodec: "av1",
			},
			SelectedOutputKind: "hls",
			TargetProfile: &playbackprofile.TargetPlaybackProfile{
				Container: "mpegts",
				Packaging: playbackprofile.PackagingTS,
				HLS: playbackprofile.HLSTarget{
					Enabled:          true,
					SegmentContainer: "mpegts",
				},
				Video: playbackprofile.VideoTarget{
					Mode:  playbackprofile.MediaModeTranscode,
					Codec: "av1",
				},
				Audio: playbackprofile.AudioTarget{
					Mode:  playbackprofile.MediaModeTranscode,
					Codec: "aac",
				},
			},
		},
	)

	if plan.applied {
		t.Fatalf("expected experimental desktop Safari AV1 TS to bypass transport rewrite, got %#v", plan)
	}
}

func TestApplyPlaybackTransportPolicy_RecordingDirectStreamFMP4Rewrite(t *testing.T) {
	dec := &decision.Decision{
		Mode: decision.ModeDirectPlay,
		Trace: decision.Trace{
			RequestedIntent: "direct",
			ResolvedIntent:  "direct",
		},
		TargetProfile: &playbackprofile.TargetPlaybackProfile{
			Container: "mpegts",
			Packaging: playbackprofile.PackagingTS,
			Video: playbackprofile.VideoTarget{
				Mode:  playbackprofile.MediaModeCopy,
				Codec: "h264",
			},
			Audio: playbackprofile.AudioTarget{
				Mode:  playbackprofile.MediaModeCopy,
				Codec: "ac3",
			},
		},
	}

	applyPlaybackTransportPolicy(
		PlaybackInfoRequest{SubjectKind: PlaybackSubjectRecording},
		capabilities.PlaybackCapabilities{
			ClientFamilyFallback: playbackprofile.ClientSafariNative,
		},
		dec,
	)

	if dec.Mode != decision.ModeDirectStream {
		t.Fatalf("expected direct stream after transport policy, got %#v", dec)
	}
	if dec.TargetProfile == nil {
		t.Fatal("expected target profile after transport policy")
	}
	if dec.TargetProfile.Container != "mp4" || dec.TargetProfile.Packaging != playbackprofile.PackagingFMP4 || dec.TargetProfile.HLS.SegmentContainer != "fmp4" {
		t.Fatalf("unexpected rewritten recording target: %#v", dec.TargetProfile)
	}
	if dec.Trace.QualityRung != string(playbackprofile.RungCompatibleHLSFMP4) {
		t.Fatalf("expected fmp4 compatible rung, got %#v", dec.Trace)
	}
	if dec.Trace.ResolvedIntent != string(playbackprofile.IntentCompatible) {
		t.Fatalf("expected compatible resolved intent, got %#v", dec.Trace)
	}
}
