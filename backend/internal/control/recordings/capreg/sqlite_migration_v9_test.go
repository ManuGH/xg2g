// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package capreg

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// A capability registry written before the family split still names
// `ios_safari_native`. The rows describe real observed capabilities, so they
// are relabelled rather than dropped: deleting them would make every device
// that had been observed start from nothing.
func TestMigrateClientFamiliesToV9_RelabelsRetiredIOSFamily(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capability.sqlite")

	store, err := NewSqliteStore(path)
	require.NoError(t, err)

	_, err = store.DB.Exec(`
		INSERT INTO capability_devices (
			device_fingerprint, client_family, client_caps_source, device_type,
			brand, product, device_name, platform, manufacturer, model,
			os_name, os_version, sdk_int, capabilities_json, capabilities_hash,
			network_json, updated_at_ms
		) VALUES (
			'fp-legacy', 'ios_safari_native', 'runtime', 'mobile',
			'apple', 'iphone', 'iPhone', 'ios', 'Apple', 'iPhone 15 Pro',
			'ios', '17.5', 0, '{}', 'hash-legacy', NULL, 1000
		)`)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	// Reopening runs the migration.
	reopened, err := NewSqliteStore(path)
	require.NoError(t, err)
	defer func() { _ = reopened.Close() }()

	var family string
	require.NoError(t, reopened.DB.QueryRow(
		`SELECT client_family FROM capability_devices WHERE device_fingerprint = 'fp-legacy'`,
	).Scan(&family))
	require.Equal(t, "ios_safari", family)

	var retired int
	require.NoError(t, reopened.DB.QueryRow(
		`SELECT COUNT(*) FROM capability_devices WHERE client_family = 'ios_safari_native'`,
	).Scan(&retired))
	require.Zero(t, retired)

	var version int
	require.NoError(t, reopened.DB.QueryRow(`PRAGMA user_version`).Scan(&version))
	require.Equal(t, SQLiteSchemaVersion, version)
}
