package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	sqlitepkg "github.com/ManuGH/xg2g/internal/persistence/sqlite"
)

type sqliteExecContext interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type sqlRowScanner interface {
	Scan(dest ...any) error
}

func marshalCapabilities(capabilities map[string]any) ([]byte, error) {
	if capabilities == nil {
		return []byte(`{}`), nil
	}
	return json.Marshal(capabilities)
}

func unmarshalCapabilities(raw []byte) (map[string]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}, nil
	}
	var capabilities map[string]any
	if err := json.Unmarshal(raw, &capabilities); err != nil {
		return nil, err
	}
	if capabilities == nil {
		capabilities = map[string]any{}
	}
	return capabilities, nil
}

func toMillis(value time.Time) int64 {
	return value.UTC().UnixMilli()
}

func toNullableMillis(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().UnixMilli()
}

func fromMillis(value int64) time.Time {
	return time.UnixMilli(value).UTC()
}

func fromNullableMillis(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	t := time.UnixMilli(value.Int64).UTC()
	return &t
}

func normalizeSQLError(err error) error {
	if err == nil {
		return nil
	}
	if sqlitepkg.IsBusyRetryable(err) {
		return err
	}
	return err
}
