package store_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/stretchr/testify/require"
)

type diagStore interface {
	PutSession(ctx context.Context, rec *model.SessionRecord) error
	ports.DiagnosticLookup
}

func diagStores(t *testing.T) map[string]diagStore {
	t.Helper()
	sqlStore, err := store.NewSqliteStore(filepath.Join(t.TempDir(), "diag.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlStore.Close() })
	return map[string]diagStore{"memory": store.NewMemoryStore(), "sqlite": sqlStore}
}

// The adapter decides between "immediate upstream release" and "warm hold" from
// this metadata, so the restart marker must survive both store implementations.
func TestDiagnosticMetadata_RestartPending(t *testing.T) {
	for name, st := range diagStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, st.PutSession(ctx, &model.SessionRecord{
				SessionID: "restart", ServiceRef: "1:0:1:0:0:0:0:0:0:0:", State: model.SessionStopped,
				Reason:      model.RClientStop,
				ContextData: map[string]string{model.CtxKeyRestartPending: "1"},
			}))
			require.NoError(t, st.PutSession(ctx, &model.SessionRecord{
				SessionID: "userstop", ServiceRef: "1:0:1:0:0:0:0:0:0:0:", State: model.SessionStopped,
				Reason: model.RClientStop,
			}))

			meta, ok := st.GetDiagnosticMetadata(ctx, "restart")
			require.True(t, ok)
			require.Equal(t, string(model.RClientStop), meta.Reason)
			require.True(t, meta.RestartPending, "restart marker must reach the diagnostic metadata")

			meta, ok = st.GetDiagnosticMetadata(ctx, "userstop")
			require.True(t, ok)
			require.Equal(t, string(model.RClientStop), meta.Reason)
			require.False(t, meta.RestartPending, "a plain user stop must not be restart-pending")
		})
	}
}
