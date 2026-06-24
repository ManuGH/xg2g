package scan

import (
	"time"

	"github.com/ManuGH/xg2g/internal/log"
)

// StartPeriodicRefresh launches a background loop that re-triggers a capability
// scan every interval, draining channels whose media-truth capability is still
// cold — never probed (scan_state NULL) or past next_retry_at. RunBackground is
// self-gating: hasPendingProbeCandidates makes it a no-op when the cache is already
// warm, and the isScanning guard makes it a no-op when a scan is already in flight.
// So this loop adds receiver load only when there is genuine cold work to drain, and
// goes quiet once every channel is warm.
//
// Why this exists: the one-shot startup scan (bootstrap.runInitialRefresh) can be
// starved indefinitely on a receiver that is in continuous use — waitForPlaybackIdle
// pauses the scan while a stream is active and ProbeDelay rate-limits each probe, so a
// large cold backlog never fully drains in a single pass. Without periodic re-triggering
// those channels stay cold forever and every first playback pays the full synchronous
// cold-tune probe in the playback-decision path.
//
// interval <= 0 disables periodic refresh (startup scan only). The loop is bound to the
// lifecycle context attached via AttachLifecycle; Stop() cancels it and waits via bgWG.
func (m *Manager) StartPeriodicRefresh(interval time.Duration) {
	if m == nil || interval <= 0 {
		return
	}

	m.lifecycleMu.Lock()
	if m.runtimeCtx == nil {
		m.lifecycleMu.Unlock()
		log.L().Warn().Msg("scan: periodic capability refresh not started (lifecycle context not attached)")
		return
	}
	if m.runtimeCtx.Err() != nil {
		m.lifecycleMu.Unlock()
		log.L().Warn().Msg("scan: periodic capability refresh not started (lifecycle already cancelled)")
		return
	}
	ctx := m.runtimeCtx
	m.bgWG.Add(1)
	m.lifecycleMu.Unlock()

	go func() {
		defer m.bgWG.Done()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		log.L().Info().Dur("interval", interval).Msg("scan: periodic capability refresh enabled")

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Self-gating: no-op when the cache is warm or a scan is already running.
				m.RunBackground()
			}
		}
	}()
}
