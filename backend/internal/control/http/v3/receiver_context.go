package v3

import (
	"context"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
)

const receiverAboutCacheTTL = 5 * time.Minute

func (s *Server) currentReceiverContext(ctx context.Context) *capreg.ReceiverContext {
	about := s.currentReceiverAbout(ctx)
	if about == nil {
		return nil
	}
	return receiverContextFromAbout(about)
}

func (s *Server) currentReceiverAbout(ctx context.Context) *openwebif.AboutInfo {
	s.mu.RLock()
	snap := s.snap
	cached := s.receiverAbout
	cachedAt := s.receiverAboutAt
	cachedEpoch := s.receiverAboutEpoch
	s.mu.RUnlock()

	if cached != nil && cachedEpoch == snap.Epoch && time.Since(cachedAt) < receiverAboutCacheTTL {
		return cached
	}

	val, err, _ := s.receiverSfg.Do("receiver-about", func() (interface{}, error) {
		s.mu.RLock()
		cached := s.receiverAbout
		cachedAt := s.receiverAboutAt
		cachedEpoch := s.receiverAboutEpoch
		currentSnap := s.snap
		s.mu.RUnlock()
		if cached != nil && cachedEpoch == currentSnap.Epoch && time.Since(cachedAt) < receiverAboutCacheTTL {
			return cached, nil
		}

		s.mu.RLock()
		cfg := s.cfg
		currentSnap = s.snap
		s.mu.RUnlock()

		owiClient := s.owi(cfg, currentSnap)
		client, ok := owiClient.(*openwebif.Client)
		if !ok || client == nil {
			return (*openwebif.AboutInfo)(nil), nil
		}

		upstreamCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		about, err := client.About(upstreamCtx)
		if err != nil {
			return nil, err
		}

		s.mu.Lock()
		s.receiverAbout = about
		s.receiverAboutAt = time.Now().UTC()
		s.receiverAboutEpoch = currentSnap.Epoch
		s.mu.Unlock()
		return about, nil
	})
	if err != nil {
		log.L().Debug().Err(err).Msg("receiver context: failed to query receiver about info")
		return nil
	}
	about, _ := val.(*openwebif.AboutInfo)
	return about
}

func receiverContextFromAbout(about *openwebif.AboutInfo) *capreg.ReceiverContext {
	if about == nil {
		return nil
	}

	info := about.Info
	ctx := &capreg.ReceiverContext{
		Platform:            "enigma2",
		Brand:               info.Brand,
		Model:               firstNonEmpty(info.Model, info.MachineBuild, info.Boxtype),
		OSName:              firstNonEmpty(info.FriendlyImageDistro, info.ImageDistro, "enigma2"),
		OSVersion:           firstNonEmpty(info.ImageVer, info.OEVer),
		KernelVersion:       strings.TrimSpace(info.KernelVer),
		EnigmaVersion:       strings.TrimSpace(info.EnigmaVer),
		WebInterfaceVersion: strings.TrimSpace(info.WebIFVer),
	}

	if ctx.Brand == "" && ctx.Model == "" && ctx.OSName == "" && ctx.OSVersion == "" && ctx.KernelVersion == "" && ctx.EnigmaVersion == "" && ctx.WebInterfaceVersion == "" {
		return nil
	}
	return ctx
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
