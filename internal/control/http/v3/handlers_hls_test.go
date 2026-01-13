package v3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/control/http/v3/recordings/artifacts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockArtifactResolver
type MockArtifactResolver struct {
	mock.Mock
}

func (m *MockArtifactResolver) ResolvePlaylist(ctx context.Context, recordingID, profile string) (artifacts.ArtifactOK, *artifacts.ArtifactError) {
	args := m.Called(ctx, recordingID, profile)
	err, _ := args.Get(1).(*artifacts.ArtifactError)
	return args.Get(0).(artifacts.ArtifactOK), err
}

func (m *MockArtifactResolver) ResolveTimeshift(ctx context.Context, recordingID, profile string) (artifacts.ArtifactOK, *artifacts.ArtifactError) {
	args := m.Called(ctx, recordingID, profile)
	err, _ := args.Get(1).(*artifacts.ArtifactError)
	return args.Get(0).(artifacts.ArtifactOK), err
}

func (m *MockArtifactResolver) ResolveSegment(ctx context.Context, recordingID, segment string) (artifacts.ArtifactOK, *artifacts.ArtifactError) {
	args := m.Called(ctx, recordingID, segment)
	err, _ := args.Get(1).(*artifacts.ArtifactError)
	return args.Get(0).(artifacts.ArtifactOK), err
}

func TestHLS_ProfilePropagation(t *testing.T) {
	tests := []struct {
		name            string
		userAgent       string
		expectedProfile string
	}{
		{
			name:            "Safari_Mac",
			userAgent:       "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.0 Safari/605.1.15",
			expectedProfile: "safari",
		},
		{
			name:            "Safari_iOS",
			userAgent:       "Mozilla/5.0 (iPhone; CPU iPhone OS 14_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.0 Mobile/15E148 Safari/604.1",
			expectedProfile: "safari",
		},
		{
			name:            "Chrome_Mac",
			userAgent:       "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/86.0.4240.111 Safari/537.36",
			expectedProfile: "generic",
		},
		{
			name:            "Empty",
			userAgent:       "",
			expectedProfile: "generic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := new(MockArtifactResolver)
			// Expect strict profile string
			svc.On("ResolvePlaylist", mock.Anything, "rec1", tt.expectedProfile).Return(artifacts.ArtifactOK{Data: []byte("ok")}, nil)

			s := &Server{artifacts: svc}
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/api/v3/recordings/rec1/playlist.m3u8", nil)
			r.Header.Set("User-Agent", tt.userAgent)

			s.GetRecordingHLSPlaylist(w, r, "rec1")

			assert.Equal(t, http.StatusOK, w.Code)
			svc.AssertExpectations(t)
		})
	}
}

func TestProfileConsistency(t *testing.T) {
	// Verify that the helper function behaves consistently
	reqDefault, _ := http.NewRequest("GET", "/", nil)
	assert.Equal(t, ClientProfileGeneric, detectClientProfile(reqDefault))

	reqSafari, _ := http.NewRequest("GET", "/", nil)
	reqSafari.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.0 Safari/605.1.15")
	assert.Equal(t, ClientProfileSafari, detectClientProfile(reqSafari))

	reqExplicit, _ := http.NewRequest("GET", "/?profile=safari", nil)
	assert.Equal(t, ClientProfileSafari, detectClientProfile(reqExplicit))
}
