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
	start := req.Offset
	if start > total {
		start = total
	}
	end := start + req.Limit
	if end > total {
		end = total
	}
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
