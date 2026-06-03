package sessions

import "context"

// ListSessionsDebug resolves the paginated session debug list for GET /sessions.
func (s *Service) ListSessionsDebug(ctx context.Context, req ListSessionsDebugRequest) (ListSessionsDebugResult, *ListSessionsDebugError) {
	store := s.deps.SessionStore()
	if store == nil {
		return ListSessionsDebugResult{}, &ListSessionsDebugError{
			Kind:    ListSessionsDebugErrorUnavailable,
			Message: "session store is not initialized",
		}
	}

	allSessions, err := store.ListSessions(ctx)
	if err != nil {
		return ListSessionsDebugResult{}, &ListSessionsDebugError{
			Kind:    ListSessionsDebugErrorInternal,
			Message: err.Error(),
			Cause:   err,
		}
	}

	total := len(allSessions)
	start := min(req.Offset, total)
	end := min(start+req.Limit, total)
	sessions := allSessions[start:end]

	return ListSessionsDebugResult{
		Sessions: sessions,
		Pagination: ListSessionsDebugPagination{
			Offset: req.Offset,
			Limit:  req.Limit,
			Total:  total,
			Count:  len(sessions),
		},
	}, nil
}
