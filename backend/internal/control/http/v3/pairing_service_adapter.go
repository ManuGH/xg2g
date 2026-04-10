package v3

import (
	v3deviceauth "github.com/ManuGH/xg2g/internal/control/http/v3/deviceauth"
	v3pairing "github.com/ManuGH/xg2g/internal/control/http/v3/pairing"
	deviceauthstore "github.com/ManuGH/xg2g/internal/domain/deviceauth/store"
)

func (s *Server) deviceAuthStore() deviceauthstore.StateStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deviceAuthStateStore == nil {
		s.deviceAuthStateStore = deviceauthstore.NewMemoryStateStore()
	}
	return s.deviceAuthStateStore
}

func (s *Server) hasDeviceAuthStore() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.deviceAuthStateStore != nil
}

func (s *Server) pairingProcessor() *v3pairing.Service {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deviceAuthStateStore == nil {
		s.deviceAuthStateStore = deviceauthstore.NewMemoryStateStore()
	}
	if s.pairingV3Service == nil {
		s.pairingV3Service = v3pairing.NewService(v3pairing.Deps{
			StateStore:                 s.deviceAuthStateStore,
			PublishedEndpointsProvider: serverPublishedEndpointProvider{s: s},
		})
	}
	return s.pairingV3Service
}

func (s *Server) deviceAuthProcessor() *v3deviceauth.Service {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deviceAuthStateStore == nil {
		s.deviceAuthStateStore = deviceauthstore.NewMemoryStateStore()
	}
	if s.deviceAuthV3Service == nil {
		s.deviceAuthV3Service = v3deviceauth.NewService(v3deviceauth.Deps{
			StateStore:                 s.deviceAuthStateStore,
			PublishedEndpointsProvider: serverPublishedEndpointProvider{s: s},
		})
	}
	return s.deviceAuthV3Service
}
