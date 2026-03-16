package sessions

import (
	"context"
	"errors"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (s fakeStore) ListSessions(ctx context.Context) ([]*model.SessionRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	return []*model.SessionRecord{
		{SessionID: "s1"},
		{SessionID: "s2"},
		{SessionID: "s3"},
	}, nil
}

func TestServiceListSessionsDebugUnavailable(t *testing.T) {
	svc := NewService(fakeDeps{})

	_, err := svc.ListSessionsDebug(context.Background(), ListSessionsDebugRequest{Offset: 0, Limit: 100})

	require.NotNil(t, err)
	assert.Equal(t, ListSessionsDebugErrorUnavailable, err.Kind)
	assert.Equal(t, "session store is not initialized", err.Message)
}

func TestServiceListSessionsDebugInternal(t *testing.T) {
	svc := NewService(fakeDeps{store: fakeStore{err: errors.New("boom")}})

	_, err := svc.ListSessionsDebug(context.Background(), ListSessionsDebugRequest{Offset: 0, Limit: 100})

	require.NotNil(t, err)
	assert.Equal(t, ListSessionsDebugErrorInternal, err.Kind)
	assert.Equal(t, "boom", err.Message)
	assert.Error(t, err.Cause)
}

func TestServiceListSessionsDebugAppliesPagination(t *testing.T) {
	svc := NewService(fakeDeps{store: fakeStore{}})

	got, err := svc.ListSessionsDebug(context.Background(), ListSessionsDebugRequest{Offset: 1, Limit: 5})

	require.Nil(t, err)
	require.Len(t, got.Sessions, 2)
	assert.Equal(t, "s2", got.Sessions[0].SessionID)
	assert.Equal(t, "s3", got.Sessions[1].SessionID)
	assert.Equal(t, 1, got.Pagination.Offset)
	assert.Equal(t, 5, got.Pagination.Limit)
	assert.Equal(t, 3, got.Pagination.Total)
	assert.Equal(t, 2, got.Pagination.Count)
}

func TestServiceListSessionsDebugOffsetPastTotal(t *testing.T) {
	svc := NewService(fakeDeps{store: fakeStore{}})

	got, err := svc.ListSessionsDebug(context.Background(), ListSessionsDebugRequest{Offset: 10, Limit: 5})

	require.Nil(t, err)
	assert.Empty(t, got.Sessions)
	assert.Equal(t, 10, got.Pagination.Offset)
	assert.Equal(t, 5, got.Pagination.Limit)
	assert.Equal(t, 3, got.Pagination.Total)
	assert.Equal(t, 0, got.Pagination.Count)
}
