package v3

import (
	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
)

type serverRecordingsDeps struct {
	s *Server
}

var _ v3recordings.Deps = (*serverRecordingsDeps)(nil)

func (d *serverRecordingsDeps) RecordingsService() v3recordings.RecordingsService {
	return d.s.recordingsModuleDeps().recordingsService
}

func (s *Server) recordingsProcessor() *v3recordings.Service {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.recordingsV3Service == nil {
		s.recordingsV3Service = v3recordings.NewService(&serverRecordingsDeps{s: s})
	}
	return s.recordingsV3Service
}
