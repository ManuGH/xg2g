package recordings

import (
	"context"
	"errors"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/clientplayback"
	"github.com/ManuGH/xg2g/internal/control/playback"
	domainrecordings "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubDeps struct {
	svc          RecordingsService
	cfg          config.AppConfig
	hostPressure playbackprofile.HostPressureAssessment
	truthSource  ChannelTruthSource
	auditSink    DecisionAuditSink
}

func (d stubDeps) RecordingsService() RecordingsService {
	return d.svc
}

func (d stubDeps) Config() config.AppConfig {
	return d.cfg
}

func (d stubDeps) ChannelTruthSource() ChannelTruthSource {
	return d.truthSource
}

func (d stubDeps) DecisionAuditSink() DecisionAuditSink {
	return d.auditSink
}

func (d stubDeps) HostPressure(context.Context) playbackprofile.HostPressureAssessment {
	return d.hostPressure
}

type stubTruthSource struct {
	getCapabilityFn func(serviceRef string) (scan.Capability, bool)
	lastServiceRef  string
	calls           int
}

func (s *stubTruthSource) GetCapability(serviceRef string) (scan.Capability, bool) {
	s.calls++
	s.lastServiceRef = serviceRef
	if s.getCapabilityFn == nil {
		return scan.Capability{}, false
	}
	return s.getCapabilityFn(serviceRef)
}

type stubDecisionAuditSink struct {
	recordFn  func(ctx context.Context, event decision.Event) error
	lastEvent decision.Event
	callCount int
}

func (s *stubDecisionAuditSink) Record(ctx context.Context, event decision.Event) error {
	s.callCount++
	s.lastEvent = event
	if s.recordFn == nil {
		return nil
	}
	return s.recordFn(ctx, event)
}

type stubRecordingsService struct {
	resolveFn       func(ctx context.Context, id, profile string) (domainrecordings.PlaybackResolution, error)
	getMediaTruthFn func(ctx context.Context, id string) (playback.MediaTruth, error)
	lastID          string
	lastProf        string
	lastTruthID     string
	resolveCalls    int
	truthCalls      int
}

func (s *stubRecordingsService) ResolvePlayback(ctx context.Context, id, profile string) (domainrecordings.PlaybackResolution, error) {
	s.resolveCalls++
	s.lastID = id
	s.lastProf = profile
	return s.resolveFn(ctx, id, profile)
}

func (s *stubRecordingsService) GetMediaTruth(ctx context.Context, id string) (playback.MediaTruth, error) {
	s.truthCalls++
	s.lastTruthID = id
	if s.getMediaTruthFn == nil {
		return playback.MediaTruth{}, nil
	}
	return s.getMediaTruthFn(ctx, id)
}

func TestService_ResolveClientPlayback_Unavailable(t *testing.T) {
	svc := NewService(stubDeps{})

	_, err := svc.ResolveClientPlayback(context.Background(), "rec1", ClientPlaybackRequest{})
	require.NotNil(t, err)
	assert.Equal(t, ClientPlaybackErrorUnavailable, err.Kind)
	assert.Equal(t, "Recordings service is not initialized", err.Message)
}

func TestService_ResolveClientPlayback_ClassifiesErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		inErr      error
		kind       ClientPlaybackErrorKind
		wantMsg    string
		retry      int
		probe      string
		checkCause bool
	}{
		{
			name:       "invalid argument",
			inErr:      domainrecordings.ErrInvalidArgument{Field: "id", Reason: "bad"},
			kind:       ClientPlaybackErrorInvalidInput,
			wantMsg:    "invalid argument id: bad",
			checkCause: true,
		},
		{
			name:       "not found",
			inErr:      domainrecordings.ErrNotFound{RecordingID: "rec1"},
			kind:       ClientPlaybackErrorNotFound,
			wantMsg:    "recording not found: rec1",
			checkCause: true,
		},
		{
			name:       "preparing",
			inErr:      domainrecordings.ErrPreparing{RecordingID: "rec1"},
			kind:       ClientPlaybackErrorPreparing,
			wantMsg:    "recording preparing: rec1",
			retry:      5,
			probe:      "in_flight",
			checkCause: true,
		},
		{
			name:       "upstream",
			inErr:      domainrecordings.ErrUpstream{Op: "resolve", Cause: errors.New("timeout")},
			kind:       ClientPlaybackErrorUpstreamUnavailable,
			wantMsg:    "upstream error in resolve: timeout",
			checkCause: true,
		},
		{
			name:       "internal",
			inErr:      errors.New("boom"),
			kind:       ClientPlaybackErrorInternal,
			wantMsg:    "An unexpected error occurred",
			checkCause: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			resolver := &stubRecordingsService{
				resolveFn: func(context.Context, string, string) (domainrecordings.PlaybackResolution, error) {
					return domainrecordings.PlaybackResolution{}, tt.inErr
				},
			}
			svc := NewService(stubDeps{svc: resolver})

			_, err := svc.ResolveClientPlayback(context.Background(), "rec1", ClientPlaybackRequest{})
			require.NotNil(t, err)
			assert.Equal(t, tt.kind, err.Kind)
			assert.Equal(t, tt.wantMsg, err.Message)
			assert.Equal(t, tt.retry, err.RetryAfterSeconds)
			assert.Equal(t, tt.probe, err.ProbeState)
			if tt.checkCause {
				assert.ErrorIs(t, err.Cause, tt.inErr)
			}
			assert.Equal(t, 1, resolver.resolveCalls)
			assert.Equal(t, "rec1", resolver.lastID)
			assert.Equal(t, "generic", resolver.lastProf)
		})
	}
}

func TestService_ResolveClientPlayback_DirectPlay(t *testing.T) {
	c := "mp4"
	v := "h264"
	a := "aac"
	dur := int64(42)

	resolver := &stubRecordingsService{
		resolveFn: func(context.Context, string, string) (domainrecordings.PlaybackResolution, error) {
			return domainrecordings.PlaybackResolution{
				Strategy:    domainrecordings.StrategyDirect,
				DurationSec: &dur,
				Container:   &c,
				VideoCodec:  &v,
				AudioCodec:  &a,
			}, nil
		},
	}

	svc := NewService(stubDeps{svc: resolver})
	req := ClientPlaybackRequest{
		DeviceProfile: &clientplayback.DeviceProfile{
			DirectPlayProfiles: []clientplayback.DirectPlayProfile{
				{
					Type:       "Video",
					Container:  "mp4,m4v",
					VideoCodec: "h264",
					AudioCodec: "aac,ac3",
				},
			},
		},
	}

	resp, err := svc.ResolveClientPlayback(context.Background(), "rec1", req)
	require.Nil(t, err)
	require.Len(t, resp.MediaSources, 1)

	ms := resp.MediaSources[0]
	assert.Equal(t, "/api/v3/recordings/rec1/stream.mp4", ms.Path)
	require.NotNil(t, ms.Container)
	assert.Equal(t, "mp4", *ms.Container)
	assert.True(t, ms.SupportsDirectPlay)
	assert.True(t, ms.SupportsDirectStream)
	assert.True(t, ms.SupportsTranscoding)
	assert.Nil(t, ms.TranscodingUrl)
	require.NotNil(t, ms.RunTimeTicks)
	assert.Equal(t, int64(42*10_000_000), *ms.RunTimeTicks)
}

func TestService_ResolveClientPlayback_TranscodeFallback(t *testing.T) {
	resolver := &stubRecordingsService{
		resolveFn: func(context.Context, string, string) (domainrecordings.PlaybackResolution, error) {
			return domainrecordings.PlaybackResolution{
				Strategy: domainrecordings.StrategyDirect,
			}, nil
		},
	}

	svc := NewService(stubDeps{svc: resolver})
	resp, err := svc.ResolveClientPlayback(context.Background(), "rec1", ClientPlaybackRequest{})
	require.Nil(t, err)
	require.Len(t, resp.MediaSources, 1)

	ms := resp.MediaSources[0]
	assert.Equal(t, "/api/v3/recordings/rec1/playlist.m3u8", ms.Path)
	assert.False(t, ms.SupportsDirectPlay)
	assert.False(t, ms.SupportsDirectStream)
	assert.True(t, ms.SupportsTranscoding)
	require.NotNil(t, ms.TranscodingUrl)
	assert.Equal(t, "/api/v3/recordings/rec1/playlist.m3u8", *ms.TranscodingUrl)
	require.NotNil(t, ms.TranscodingContainer)
	assert.Equal(t, "m3u8", *ms.TranscodingContainer)
	require.NotNil(t, ms.TranscodingSubProtocol)
	assert.Equal(t, "hls", *ms.TranscodingSubProtocol)
}
