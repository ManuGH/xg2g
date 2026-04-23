package bootstrap

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/openwebif"
)

type backgroundScanTestStore struct {
	sessions []*model.SessionRecord
	err      error
	calls    int
}

func (s *backgroundScanTestStore) ListSessions(ctx context.Context) ([]*model.SessionRecord, error) {
	s.calls++
	return s.sessions, s.err
}

type backgroundScanTestReceiver struct {
	about       *openwebif.AboutInfo
	aboutErr    error
	status      *openwebif.StatusInfo
	statusErr   error
	aboutCalls  int
	statusCalls int
}

func (r *backgroundScanTestReceiver) About(ctx context.Context) (*openwebif.AboutInfo, error) {
	r.aboutCalls++
	return r.about, r.aboutErr
}

func (r *backgroundScanTestReceiver) GetStatusInfo(ctx context.Context) (*openwebif.StatusInfo, error) {
	r.statusCalls++
	return r.status, r.statusErr
}

func testAboutInfo(tuners []openwebif.AboutTuner, streams any) *openwebif.AboutInfo {
	info := &openwebif.AboutInfo{}
	info.Info.Tuners = tuners
	info.Info.Streams = streams
	return info
}

func TestBackgroundScanPlaybackDetector_UsesSessionTruthFirst(t *testing.T) {
	store := &backgroundScanTestStore{
		sessions: []*model.SessionRecord{{
			SessionID:     "s1",
			State:         model.SessionStarting,
			CreatedAtUnix: time.Now().Unix(),
			UpdatedAtUnix: time.Now().Unix(),
		}},
	}
	receiver := &backgroundScanTestReceiver{}

	active, err := newBackgroundScanPlaybackDetector(store, receiver)(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !active {
		t.Fatal("expected active playback from session store")
	}
	if receiver.aboutCalls != 0 || receiver.statusCalls != 0 {
		t.Fatal("receiver should not be queried when session truth is already active")
	}
}

func TestBackgroundScanPlaybackDetector_FallsBackToReceiverStatus(t *testing.T) {
	store := &backgroundScanTestStore{}
	receiver := &backgroundScanTestReceiver{
		status: &openwebif.StatusInfo{IsStreaming: "true"},
	}

	active, err := newBackgroundScanPlaybackDetector(store, receiver)(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !active {
		t.Fatal("expected active playback from receiver status")
	}
}

func TestBackgroundScanPlaybackDetector_SuppressesStaleTunerStreamingFlags(t *testing.T) {
	store := &backgroundScanTestStore{}
	receiver := &backgroundScanTestReceiver{
		status: &openwebif.StatusInfo{IsStreaming: "false"},
		about:  testAboutInfo([]openwebif.AboutTuner{{Name: "Tuner A", Stream: "1:0:19:abc"}}, []any{}),
	}

	active, err := newBackgroundScanPlaybackDetector(store, receiver)(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if active {
		t.Fatal("expected stale tuner streaming flag to be suppressed")
	}
}

func TestBackgroundScanPlaybackDetector_UsesLegacyTunerSignalWhenGlobalSignalUnknown(t *testing.T) {
	store := &backgroundScanTestStore{}
	receiver := &backgroundScanTestReceiver{
		about: testAboutInfo([]openwebif.AboutTuner{{Name: "Tuner A", Stream: "1:0:19:abc"}}, nil),
	}

	active, err := newBackgroundScanPlaybackDetector(store, receiver)(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !active {
		t.Fatal("expected tuner streaming signal to be used when no global signal exists")
	}
}

func TestBackgroundScanPlaybackDetector_ReturnsReceiverErrorWhenAllFallbacksFail(t *testing.T) {
	store := &backgroundScanTestStore{}
	receiver := &backgroundScanTestReceiver{
		aboutErr:  errors.New("about failed"),
		statusErr: errors.New("status failed"),
	}

	active, err := newBackgroundScanPlaybackDetector(store, receiver)(context.Background())
	if err == nil {
		t.Fatal("expected combined receiver error")
	}
	if active {
		t.Fatal("expected inactive playback when receiver probes fail")
	}
}
