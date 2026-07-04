package manager

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/infra/media/ffmpeg"
	"github.com/ManuGH/xg2g/internal/log"
)

func (o *Orchestrator) ScanAndAdoptOrphans(ctx context.Context) {
	states, err := ffmpeg.ReadWorkerStates()
	if err != nil {
		log.L().Warn().Err(err).Msg("Orphan scanner failed to read worker states")
		return
	}

	for _, state := range states {
		alive := false
		process, err := os.FindProcess(state.PID)
		if err == nil {
			err = process.Signal(syscall.Signal(0))
			if err == nil {
				alive = true
			}
		}

		if !alive {
			log.L().Info().Str("session_id", state.SessionID).Int("pid", state.PID).Msg("Orphan scanner: dead session, cleaning tmpfs")
			_ = os.RemoveAll(filepath.Join("/dev/shm/xg2g/sessions", state.SessionID))
			continue
		}

		rec, _ := o.Store.GetSession(ctx, state.SessionID)
		if rec != nil && !rec.State.IsTerminal() {
			continue
		}

		log.L().Info().Str("session_id", state.SessionID).Int("pid", state.PID).Msg("Orphan scanner: adopting session")

		now := time.Now().Unix()
		adopted := &model.SessionRecord{
			SessionID:          state.SessionID,
			ServiceRef:         state.ServiceRef,
			Profile:            ports.ProfileSpec{Name: state.ProfileID, TranscodeVideo: true},
			State:              model.SessionReady,
			PipelineState:      model.PipeServing,
			Reason:             model.RNone,
			CreatedAtUnix:      state.CreatedAt,
			UpdatedAtUnix:      now,
			LastAccessUnix:     now,
			ExpiresAtUnix:      now + 30, // Sweeper will extend this when browser requests playlists
			LeaseExpiresAtUnix: now + 30,
		}
		_ = o.Store.PutSession(ctx, adopted)
	}
}
