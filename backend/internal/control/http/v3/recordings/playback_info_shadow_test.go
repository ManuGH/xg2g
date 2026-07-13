package recordings

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/playbackshadow"
	domainrecordings "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockShadowObserver struct {
	observations []playbackshadow.ShadowObservation
}

func (m *mockShadowObserver) TryObserve(obs playbackshadow.ShadowObservation) bool {
	m.observations = append(m.observations, obs)
	return true
}

type stubChannelSource struct {
	cap   scan.Capability
	found bool
}

func (s *stubChannelSource) GetCapability(serviceRef string) (scan.Capability, bool) {
	return s.cap, s.found
}

func TestResolvePlaybackInfo_ShadowObserver_RecordingAndOff(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/demo.ts"
	recordingID := domainrecordings.EncodeRecordingID(serviceRef)
	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
			return playback.MediaTruth{
				Status:     playback.MediaStatusReady,
				Container:  "mp4",
				VideoCodec: "h264",
				AudioCodec: "aac",
				Width:      1920,
				Height:     1080,
				FPS:        25,
			}, nil
		},
	}

	obs := &mockShadowObserver{}

	svc := NewService(stubDeps{
		svc: recSvc,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	}, WithPlannerShadowObserver(obs))

	// Test Shadow ON (recording)
	res1, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   recordingID,
		SubjectKind: PlaybackSubjectRecording,
		APIVersion:  "v3.1",
		SchemaType:  "compact",
		RequestID:   "req-1",
	})
	require.Nil(t, err)
	require.NotNil(t, res1.Decision)
	assert.Equal(t, 1, len(obs.observations))
	assert.Equal(t, "recordings", obs.observations[0].Evidence.Provenance)
	assert.Equal(t, "ok", obs.observations[0].Evidence.Confidence)

	// Test Shadow OFF
	svcOff := NewService(stubDeps{
		svc: recSvc,
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	}) // Without observer option
	res2, err := svcOff.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   recordingID,
		SubjectKind: PlaybackSubjectRecording,
		APIVersion:  "v3.1",
		SchemaType:  "compact",
		RequestID:   "req-2",
	})
	require.Nil(t, err)

	// Decisions should be identical
	assert.Equal(t, res1.Decision.Mode, res2.Decision.Mode)
	assert.Equal(t, res1.Decision.Selected, res2.Decision.Selected)
}

func TestResolvePlaybackInfo_ShadowObserver_Live(t *testing.T) {
	serviceRef := "1:0:1:2B66:3F3:1:C00000:0:0:0:"
	recSvc := &stubRecordingsService{}

	obs := &mockShadowObserver{}

	svc := NewService(stubDeps{
		svc: recSvc,
		truthSource: &stubChannelSource{
			cap: scan.Capability{
				ServiceRef: serviceRef,
				Container:  "mpegts",
				VideoCodec: "h264",
				AudioCodec: "aac",
				LastScan:   time.Now().UTC(),
			},
			found: true,
		},
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	}, WithPlannerShadowObserver(obs))

	res, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   serviceRef,
		SubjectKind: PlaybackSubjectLive,
		APIVersion:  "v3.1",
		SchemaType:  "compact",
		RequestID:   "req-3",
	})
	require.Nil(t, err)
	require.NotNil(t, res.Decision)
	assert.Equal(t, 1, len(obs.observations))
	assert.Equal(t, "live_scan", obs.observations[0].Evidence.Provenance)

	// Verify shadow OFF produces 0 observations and identical decision for live
	obsOff := &mockShadowObserver{}
	svcOff := NewService(stubDeps{
		svc: recSvc,
		truthSource: &stubChannelSource{
			cap: scan.Capability{
				ServiceRef: serviceRef,
				Container:  "mpegts",
				VideoCodec: "h264",
				AudioCodec: "aac",
				LastScan:   time.Now().UTC(),
			},
			found: true,
		},
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	}) // No observer option passed
	resOff, err := svcOff.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:   serviceRef,
		SubjectKind: PlaybackSubjectLive,
		APIVersion:  "v3.1",
		SchemaType:  "compact",
		RequestID:   "req-4",
	})
	require.Nil(t, err)
	assert.Equal(t, 0, len(obsOff.observations))
	assert.Equal(t, res.Decision.Mode, resOff.Decision.Mode)
	assert.Equal(t, res.Decision.Selected, resOff.Decision.Selected)
}

func TestResolvePlaybackInfo_Shadow_NoHLS_DeniesTranscodeParity(t *testing.T) {
	serviceRef := "1:0:1:2B66:3F3:1:C00000:0:0:0:"
	recSvc := &stubRecordingsService{}
	obs := &mockShadowObserver{}

	svc := NewService(stubDeps{
		svc: recSvc,
		truthSource: &stubChannelSource{
			cap: scan.Capability{
				ServiceRef: serviceRef,
				Container:  "mpegts",
				VideoCodec: "h264",
				AudioCodec: "mp2", // Requires audio transcode
				LastScan:   time.Now().UTC(),
			},
			found: true,
		},
		cfg: config.AppConfig{
			FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
			HLS:    config.HLSConfig{Root: "/tmp/hls"},
		},
	}, WithPlannerShadowObserver(obs))

	// Send request with NO supportsHls or hlsEngines -> legacy returns deny
	reqCaps := capabilities.PlaybackCapabilities{
		CapabilitiesVersion: 1,
		Containers:          []string{"ts", "hls"},
		VideoCodecs:         []string{"h264"},
		AudioCodecs:         []string{"aac"},
	}

	res, err := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
		SubjectID:    serviceRef,
		SubjectKind:  PlaybackSubjectLive,
		APIVersion:   "v3.1",
		SchemaType:   "compact",
		RequestID:    "req-parity-1",
		Capabilities: &reqCaps,
	})
	require.Nil(t, err)
	assert.Equal(t, decision.ModeDeny, res.Decision.Mode)

	require.Len(t, obs.observations, 1)
	// Verify Planner also evaluated and output deny due to missing HLS capability
	plannerRes, planErr := playbackplanner.Plan(obs.observations[0].Evidence)
	require.NoError(t, planErr)
	assert.Equal(t, playbackplanner.DecisionDeny, plannerRes.Plan.Decision)
	assert.Equal(t, "deny", plannerRes.Plan.Outcome)
	assert.Equal(t, "none", plannerRes.Plan.Mode)
}

func TestResolvePlaybackInfo_Shadow_Phase1Fixtures_Parity(t *testing.T) {
	casesDir := filepath.Join("..", "..", "..", "..", "..", "testdata", "contract", "p4_1", "cases")
	entries, err := os.ReadDir(casesDir)
	require.NoError(t, err, "Failed to read test cases directory")

	var caseFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".input.json") && !strings.HasPrefix(entry.Name(), ".") {
			caseFiles = append(caseFiles, entry.Name())
		}
	}
	require.Len(t, caseFiles, 8, "Must verify exactly the frozen 8 Phase-1 fixtures")

	for _, caseFile := range caseFiles {
		t.Run(caseFile, func(t *testing.T) {
			casePath := filepath.Join(casesDir, caseFile)
			caseData, err := os.ReadFile(casePath)
			require.NoError(t, err)

			var tc struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Input       struct {
					Source struct {
						Container   string `json:"container"`
						VideoCodec  string `json:"video_codec"`
						AudioCodec  string `json:"audio_codec"`
						BitrateKbps int    `json:"bitrate_kbps"`
						Resolution  string `json:"resolution"`
					} `json:"source"`
					Capabilities *struct {
						CapabilitiesVersion int      `json:"capabilities_version"`
						Container           []string `json:"container"`
						VideoCodecs         []string `json:"video_codecs"`
						AudioCodecs         []string `json:"audio_codecs"`
						SupportsHLS         bool     `json:"supports_hls"`
						SupportsRange       bool     `json:"supports_range"`
						DeviceType          string   `json:"device_type"`
					} `json:"capabilities"`
					Policy struct {
						AllowTranscode bool `json:"allow_transcode"`
					} `json:"policy"`
					APIVersion string `json:"api_version"`
				} `json:"input"`
				Expected struct {
					Status  int `json:"status"`
					Problem *struct {
						Status int    `json:"status"`
						Code   string `json:"code"`
					} `json:"problem"`
					Decision *struct {
						Mode string `json:"mode"`
					} `json:"decision"`
				} `json:"expected"`
			}
			require.NoError(t, json.Unmarshal(caseData, &tc))
			if tc.Expected.Status != 200 {
				require.NotNil(t, tc.Expected.Problem)
				assert.Contains(t, []string{"capabilities_missing", "capabilities_invalid", "decision_ambiguous"}, tc.Expected.Problem.Code)
				// These are HTTP contract failures, not valid planning evidence. The
				// existing P4.1 boundary suite owns their exact status/problem body;
				// shadow mode intentionally begins only after boundary validation.
				return
			}

			serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/" + tc.Name + ".ts"
			recordingID := domainrecordings.EncodeRecordingID(serviceRef)
			recSvc := &stubRecordingsService{
				getMediaTruthFn: func(context.Context, string) (playback.MediaTruth, error) {
					width, height := 0, 0
					if _, scanErr := fmt.Sscanf(tc.Input.Source.Resolution, "%dx%d", &width, &height); scanErr != nil {
						width, height = 0, 0
					}
					return playback.MediaTruth{
						Status:      playback.MediaStatusReady,
						Container:   tc.Input.Source.Container,
						VideoCodec:  tc.Input.Source.VideoCodec,
						AudioCodec:  tc.Input.Source.AudioCodec,
						BitrateKbps: tc.Input.Source.BitrateKbps,
						Width:       width,
						Height:      height,
						FPS:         25,
					}, nil
				},
			}

			obs := &mockShadowObserver{}
			svc := NewService(stubDeps{
				svc: recSvc,
				cfg: config.AppConfig{
					FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
					HLS:    config.HLSConfig{Root: "/tmp/hls"},
				},
			}, WithPlannerShadowObserver(obs))

			var caps *capabilities.PlaybackCapabilities
			if tc.Input.Capabilities != nil {
				allowTranscode := tc.Input.Policy.AllowTranscode
				caps = &capabilities.PlaybackCapabilities{
					CapabilitiesVersion: tc.Input.Capabilities.CapabilitiesVersion,
					Containers:          tc.Input.Capabilities.Container,
					VideoCodecs:         tc.Input.Capabilities.VideoCodecs,
					AudioCodecs:         tc.Input.Capabilities.AudioCodecs,
					SupportsHLS:         tc.Input.Capabilities.SupportsHLS,
					SupportsHLSExplicit: true,
					SupportsRange:       boolPtr(tc.Input.Capabilities.SupportsRange),
					DeviceType:          tc.Input.Capabilities.DeviceType,
					AllowTranscode:      &allowTranscode,
				}
			}

			result, resolveErr := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
				SubjectID:    recordingID,
				SubjectKind:  PlaybackSubjectRecording,
				APIVersion:   tc.Input.APIVersion,
				SchemaType:   "compact",
				RequestID:    "req-phase1-" + tc.Name,
				Capabilities: caps,
			})
			require.Nil(t, resolveErr)
			require.NotNil(t, result.Decision)
			require.NotNil(t, tc.Expected.Decision)
			assert.Equal(t, tc.Expected.Decision.Mode, string(result.Decision.Mode))

			require.Len(t, obs.observations, 1, "Expected shadow observation to be emitted for fixture %s", tc.Name)
			ob := obs.observations[0]

			plannerRes, planErr := playbackplanner.Plan(ob.Evidence)
			require.NoError(t, planErr)
			plannerComp := playbackshadow.ComparableFromPlanner(plannerRes.Plan)

			rawDiffs := playbackshadow.DiffComparablePlans(ob.Legacy, plannerComp)
			unexplained := playbackshadow.UnexplainedDiffCodes(ob.Legacy, plannerComp)
			t.Logf("fixture=%s raw_diffs=%v", tc.Name, rawDiffs)
			require.Empty(t, unexplained, "Expected 0 unexplained diffs for Phase-1 fixture %s, got: %v", tc.Name, unexplained)
		})
	}
}

func TestResolvePlaybackInfo_Shadow_RealScenarios_Matrix(t *testing.T) {
	serviceRef := "1:0:1:2B66:3F3:1:C00000:0:0:0:"
	recordingRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/matrix.ts"
	recordingID := domainrecordings.EncodeRecordingID(recordingRef)

	recSvc := &stubRecordingsService{
		getMediaTruthFn: func(ctx context.Context, id string) (playback.MediaTruth, error) {
			return playback.MediaTruth{
				Status:      playback.MediaStatusReady,
				Container:   "mp4",
				VideoCodec:  "h264",
				AudioCodec:  "aac",
				BitrateKbps: 4000,
				Width:       1920,
				Height:      1080,
				FPS:         25,
			}, nil
		},
	}

	runCase := func(t *testing.T, name, clientProfile string, isLive bool, sourceCap scan.Capability, caps capabilities.PlaybackCapabilities) {
		t.Helper()
		caps.SupportsHLSExplicit = true
		caps.ClientFamilyFallback = clientProfile
		obs := &mockShadowObserver{}
		svc := NewService(stubDeps{
			svc: recSvc,
			truthSource: &stubChannelSource{
				cap:   sourceCap,
				found: true,
			},
			cfg: config.AppConfig{
				FFmpeg: config.FFmpegConfig{Bin: "/usr/bin/ffmpeg"},
				HLS:    config.HLSConfig{Root: "/tmp/hls"},
			},
		}, WithPlannerShadowObserver(obs))

		subjID := serviceRef
		subjKind := PlaybackSubjectLive
		if !isLive {
			subjID = recordingID
			subjKind = PlaybackSubjectRecording
		}

		result, resolveErr := svc.ResolvePlaybackInfo(context.Background(), PlaybackInfoRequest{
			SubjectID:     subjID,
			SubjectKind:   subjKind,
			APIVersion:    "v3.1",
			SchemaType:    "compact",
			RequestID:     "req-matrix-" + name,
			ClientProfile: clientProfile,
			Capabilities:  &caps,
		})
		require.Nil(t, resolveErr)
		require.NotNil(t, result.Decision)

		require.Len(t, obs.observations, 1, "Expected shadow observation for scenario %s", name)
		ob := obs.observations[0]
		plannerRes, planErr := playbackplanner.Plan(ob.Evidence)
		require.NoError(t, planErr)
		plannerComp := playbackshadow.ComparableFromPlanner(plannerRes.Plan)

		t.Logf("Scenario %s Legacy: %+v\nPlanner: %+v", name, ob.Legacy, plannerComp)

		rawDiffs := playbackshadow.DiffComparablePlans(ob.Legacy, plannerComp)
		unexplained := playbackshadow.UnexplainedDiffCodes(ob.Legacy, plannerComp)
		t.Logf("Scenario %s raw diffs: %v", name, rawDiffs)
		require.Empty(t, unexplained, "Expected exactly 0 unexplained diffs for scenario %s, got: %v", name, unexplained)
	}

	t.Run("Safari_Native_H264", func(t *testing.T) {
		runCase(t, "Safari_Native_H264", playbackprofile.ClientSafariNative, true,
			scan.Capability{ServiceRef: serviceRef, Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac", BitrateKbps: 5000, Width: 1920, Height: 1080, FPS: 25, LastScan: time.Now().UTC()},
			capabilities.PlaybackCapabilities{
				CapabilitiesVersion: 1,
				Containers:          []string{"mp4", "mpegts", "hls"},
				VideoCodecs:         []string{"h264"},
				AudioCodecs:         []string{"aac"},
				SupportsHLS:         true,
				HLSEngines:          []string{"native"},
				PreferredHLSEngine:  "native",
			})
	})

	t.Run("Safari_Native_HEVC_4K", func(t *testing.T) {
		allow := true
		runCase(t, "Safari_Native_HEVC_4K", playbackprofile.ClientSafariNative, true,
			scan.Capability{ServiceRef: serviceRef, Container: "mpegts", VideoCodec: "hevc", AudioCodec: "aac", BitrateKbps: 25000, Width: 3840, Height: 2160, FPS: 50, LastScan: time.Now().UTC()},
			capabilities.PlaybackCapabilities{
				CapabilitiesVersion: 1,
				Containers:          []string{"mp4", "hls"},
				VideoCodecs:         []string{"h264", "hevc"},
				AudioCodecs:         []string{"aac"},
				SupportsHLS:         true,
				AllowTranscode:      &allow,
				HLSEngines:          []string{"native"},
				PreferredHLSEngine:  "native",
				MaxVideo:            &capabilities.MaxVideo{Width: 3840, Height: 2160, Fps: 60},
			})
	})

	t.Run("iOS_Safari", func(t *testing.T) {
		runCase(t, "iOS_Safari", playbackprofile.ClientIOSSafariNative, true,
			scan.Capability{ServiceRef: serviceRef, Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac", BitrateKbps: 5000, Width: 1920, Height: 1080, FPS: 25, LastScan: time.Now().UTC()},
			capabilities.PlaybackCapabilities{
				CapabilitiesVersion: 1,
				Containers:          []string{"mp4", "hls"},
				VideoCodecs:         []string{"h264", "hevc"},
				AudioCodecs:         []string{"aac"},
				SupportsHLS:         true,
				HLSEngines:          []string{"native"},
				PreferredHLSEngine:  "native",
			})
	})

	t.Run("Chromium_Live_HLS_Remux", func(t *testing.T) {
		runCase(t, "Chromium_Live_HLS_Remux", playbackprofile.ClientChromiumHLSJS, true,
			scan.Capability{ServiceRef: serviceRef, Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac", BitrateKbps: 4500, Width: 1920, Height: 1080, FPS: 25, LastScan: time.Now().UTC()},
			capabilities.PlaybackCapabilities{
				CapabilitiesVersion: 1,
				Containers:          []string{"webm", "mp4", "hls"},
				VideoCodecs:         []string{"h264", "vp9"},
				AudioCodecs:         []string{"aac", "opus"},
				SupportsHLS:         true,
				HLSEngines:          []string{"hlsjs"},
				PreferredHLSEngine:  "hlsjs",
			})
	})

	t.Run("WAN_Constrained_Downlink", func(t *testing.T) {
		allow := true
		runCase(t, "WAN_Constrained_Downlink", playbackprofile.ClientChromiumHLSJS, true,
			scan.Capability{ServiceRef: serviceRef, Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac", BitrateKbps: 8000, Width: 1920, Height: 1080, FPS: 25, LastScan: time.Now().UTC()},
			capabilities.PlaybackCapabilities{
				CapabilitiesVersion: 1,
				Containers:          []string{"hls"},
				VideoCodecs:         []string{"h264"},
				AudioCodecs:         []string{"aac"},
				SupportsHLS:         true,
				AllowTranscode:      &allow,
				NetworkContext: &capabilities.NetworkContext{
					DownlinkKbps: 1500,
				},
			})
	})

	t.Run("Dirty_DVB_Stream_Remux", func(t *testing.T) {
		allow := true
		runCase(t, "Dirty_DVB_Stream_Remux", playbackprofile.ClientSafariNative, true,
			scan.Capability{ServiceRef: serviceRef, Container: "mpegts", VideoCodec: "h264", AudioCodec: "mp2", BitrateKbps: 4000, Width: 1920, Height: 1080, FPS: 25, SignalFPS: 50, Interlaced: true, FieldOrder: "tff", LastScan: time.Now().UTC()},
			capabilities.PlaybackCapabilities{
				CapabilitiesVersion: 1,
				Containers:          []string{"hls"},
				VideoCodecs:         []string{"h264"},
				AudioCodecs:         []string{"aac"},
				SupportsHLS:         true,
				AllowTranscode:      &allow,
			})
	})

	t.Run("Recording_Seekable_DirectPlay", func(t *testing.T) {
		runCase(t, "Recording_Seekable_DirectPlay", playbackprofile.ClientSafariNative, false,
			scan.Capability{},
			capabilities.PlaybackCapabilities{
				CapabilitiesVersion: 1,
				Containers:          []string{"mp4", "hls"},
				VideoCodecs:         []string{"h264"},
				AudioCodecs:         []string{"aac"},
				SupportsHLS:         true,
				SupportsRange:       boolPtr(true),
			})
	})

	t.Run("No_Transcode_Policy_Denies_Incompatible", func(t *testing.T) {
		deny := false
		runCase(t, "No_Transcode_Policy_Denies_Incompatible", playbackprofile.ClientChromiumHLSJS, true,
			scan.Capability{ServiceRef: serviceRef, Container: "mpegts", VideoCodec: "hevc", AudioCodec: "ac3", BitrateKbps: 6000, LastScan: time.Now().UTC()},
			capabilities.PlaybackCapabilities{
				CapabilitiesVersion: 1,
				Containers:          []string{"mp4", "hls"},
				VideoCodecs:         []string{"h264"},
				AudioCodecs:         []string{"aac"},
				SupportsHLS:         true,
				AllowTranscode:      &deny,
			})
	})
}
