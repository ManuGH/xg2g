package recordings

import (
	"context"
	"testing"

	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/stretchr/testify/assert"
)

func TestService_SourceSnapshotForRequest_LiveUnverified(t *testing.T) {
	svc := NewService(stubDeps{
		receiver: &capreg.ReceiverContext{
			Platform:  "enigma2",
			Brand:     "vuplus",
			Model:     "uno4kse",
			OSName:    "openatv",
			OSVersion: "7.4",
		},
	})

	snapshot := svc.sourceSnapshotForRequest(context.Background(), "1:0:1:2B66:3F3:1:C00000:0:0:0:", PlaybackInfoRequest{
		SubjectKind: PlaybackSubjectLive,
	}, playback.MediaTruth{
		Status:     playback.MediaStatusReady,
		Container:  "mpegts",
		VideoCodec: "h264",
		AudioCodec: "aac",
	})

	assert.Equal(t, "live", snapshot.SubjectKind)
	assert.Equal(t, "live_unverified", snapshot.Origin)
	assert.Equal(t, "mpegts", snapshot.Container)
	assert.NotNil(t, snapshot.ReceiverContext)
	assert.Equal(t, "openatv", snapshot.ReceiverContext.OSName)
	assert.Equal(t, "7.4", snapshot.ReceiverContext.OSVersion)
	assert.ElementsMatch(t, []string{
		"live_truth_unverified",
		"scanner_unavailable",
		"missing_dimensions",
		"missing_fps",
	}, snapshot.ProblemFlags)
}

func TestService_SourceSnapshotForRequest_LiveScanTruth(t *testing.T) {
	truthSource := &stubTruthSource{
		getCapabilityFn: func(serviceRef string) (scan.Capability, bool) {
			return scan.Capability{
				ServiceRef: serviceRef,
				State:      scan.CapabilityStateOK,
				Container:  "ts",
				VideoCodec: "hevc",
				AudioCodec: "ac3",
				Width:      3840,
				Height:     2160,
				FPS:        50,
				Interlaced: true,
			}, true
		},
	}
	svc := NewService(stubDeps{truthSource: truthSource})

	snapshot := svc.sourceSnapshotForRequest(context.Background(), "1:0:1:2B66:3F3:1:C00000:0:0:0:", PlaybackInfoRequest{
		SubjectKind: PlaybackSubjectLive,
	}, playback.MediaTruth{
		Status:     playback.MediaStatusReady,
		Container:  "ts",
		VideoCodec: "hevc",
		AudioCodec: "ac3",
		Width:      3840,
		Height:     2160,
		FPS:        50,
		Interlaced: true,
	})

	assert.Equal(t, 1, truthSource.calls)
	assert.Equal(t, "live_scan", snapshot.Origin)
	assert.ElementsMatch(t, []string{"interlaced"}, snapshot.ProblemFlags)
}

func TestService_SourceSnapshotForRequest_RecordingTruth(t *testing.T) {
	svc := NewService(stubDeps{})

	snapshot := svc.sourceSnapshotForRequest(context.Background(), "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie.ts", PlaybackInfoRequest{
		SubjectKind: PlaybackSubjectRecording,
	}, playback.MediaTruth{
		Status:     playback.MediaStatusReady,
		Container:  "mpegts",
		VideoCodec: "mpeg2",
		AudioCodec: "mp2",
		Width:      720,
		Height:     576,
		FPS:        25,
	})

	assert.Equal(t, "recording_truth", snapshot.Origin)
	assert.Empty(t, snapshot.ProblemFlags)
}
