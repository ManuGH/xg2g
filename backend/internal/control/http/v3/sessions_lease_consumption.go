// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import "context"

func (s *Server) renewLeaseFromConsumption(ctx context.Context, sessionID string) {
	if s == nil {
		return
	}
	s.sessionsProcessor().RenewLeaseFromConsumption(ctx, sessionID)
}
