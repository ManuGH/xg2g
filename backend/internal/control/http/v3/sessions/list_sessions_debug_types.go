package sessions

import "github.com/ManuGH/xg2g/internal/domain/session/model"

// ListSessionsDebugRequest is the transport-neutral request payload for debug session listing.
type ListSessionsDebugRequest struct {
	Offset int
	Limit  int
}

// ListSessionsDebugPagination holds pagination metadata for the debug session list.
type ListSessionsDebugPagination struct {
	Offset int
	Limit  int
	Total  int
	Count  int
}

// ListSessionsDebugResult holds the paginated sessions and metadata needed by the HTTP adapter.
type ListSessionsDebugResult struct {
	Sessions   []*model.SessionRecord
	Pagination ListSessionsDebugPagination
}

type ListSessionsDebugErrorKind uint8

const (
	ListSessionsDebugErrorUnavailable ListSessionsDebugErrorKind = iota
	ListSessionsDebugErrorInternal
)

// ListSessionsDebugError captures transport-neutral failures from session debug listing.
type ListSessionsDebugError struct {
	Kind    ListSessionsDebugErrorKind
	Message string
	Cause   error
}

func (e *ListSessionsDebugError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return "list sessions debug error"
}
