package scan

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestManager_MergeFailedAttempt_ClassifiesInactiveEventFeed(t *testing.T) {
	manager := NewManager(NewMemoryStore(), t.TempDir()+"/playlist.m3u", nil)
	now := time.Now().UTC()

	cap := manager.mergeFailedAttempt(Capability{}, false, "1:0:1:EVENT", "Sky Sport Austria 3", now, errors.New("ffprobe failed: signal: killed (stderr: )"))

	assert.Equal(t, CapabilityStateInactiveEventFeed, cap.State)
	assert.Equal(t, "ffprobe failed: signal: killed (stderr: )", cap.FailureReason)
	assert.True(t, cap.NextRetryAt.Equal(now.Add(failureRetryWindow)))
}

func TestManager_MergeFailedAttempt_LeavesNonEventSourceFailed(t *testing.T) {
	manager := NewManager(NewMemoryStore(), t.TempDir()+"/playlist.m3u", nil)
	now := time.Now().UTC()

	cap := manager.mergeFailedAttempt(Capability{}, false, "1:0:1:LINEAR", "EUROSPORT 2", now, errors.New("ffprobe failed: signal: killed (stderr: )"))

	assert.Equal(t, CapabilityStateFailed, cap.State)
	assert.Equal(t, "ffprobe failed: signal: killed (stderr: )", cap.FailureReason)
}

func TestManager_CapabilityFromProbe_ClassifiesInactiveEventNoMetadata(t *testing.T) {
	manager := NewManager(NewMemoryStore(), t.TempDir()+"/playlist.m3u", nil)
	now := time.Now().UTC()

	cap := manager.capabilityFromProbe(Capability{}, false, "1:0:1:EVENT", "Sky Sport 8", now, nil)

	assert.Equal(t, CapabilityStateInactiveEventFeed, cap.State)
	assert.Equal(t, "inactive_event_feed_no_media_metadata", cap.FailureReason)
}
