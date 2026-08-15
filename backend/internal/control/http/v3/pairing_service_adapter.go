package v3

import (
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
			DeviceEnroller:             identityDeviceEnroller{server: s},
			PublishedEndpointsProvider: serverPublishedEndpointProvider{s: s},
		})
	}
	return s.pairingV3Service
}
