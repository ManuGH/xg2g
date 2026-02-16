// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"reflect"

	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/verification"
)

// WireV3Overrides applies optional v3 override dependencies through one typed entrypoint.
func (s *Server) WireV3Overrides(overrides V3Overrides) {
	if !isNilInterface(overrides.VerificationStore) {
		s.mu.Lock()
		s.verificationStore = overrides.VerificationStore
		s.mu.Unlock()
	}

	s.mu.RLock()
	handler := s.v3Handler
	cfg := s.cfg
	owiClient := s.owiClient
	resumeStore := s.v3RuntimeDeps.ResumeStore
	vodManager := s.vodManager
	s.mu.RUnlock()

	if !isNilInterface(overrides.VODProber) && vodManager != nil {
		vodManager.SetProber(overrides.VODProber)
	}

	var resolvedService recservice.Service
	updatedService := false

	if !isNilInterface(overrides.Resolver) {
		if handler != nil {
			handler.SetResolver(overrides.Resolver)
		}
		if isNilInterface(overrides.RecordingsService) {
			owiAdapter := v3.NewOWIAdapter(owiClient)
			resumeAdapter := v3.NewResumeAdapter(resumeStore)
			recSvc, err := recservice.NewService(&cfg, vodManager, overrides.Resolver, owiAdapter, resumeAdapter, overrides.Resolver)
			if err != nil {
				log.L().Error().Err(err).Msg("failed to re-initialize recordings service")
			} else {
				resolvedService = recSvc
				updatedService = true
			}
		}
	}

	if !isNilInterface(overrides.RecordingsService) {
		resolvedService = overrides.RecordingsService
		updatedService = true
	}

	if updatedService {
		s.mu.Lock()
		s.recordingsService = resolvedService
		s.mu.Unlock()
		s.syncV3HandlerDependencies()
	}
}

// SetVerificationStore is a compatibility wrapper for override wiring call sites.
func (s *Server) SetVerificationStore(store verification.Store) {
	s.WireV3Overrides(V3Overrides{VerificationStore: store})
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
