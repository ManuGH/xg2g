// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"github.com/ManuGH/xg2g/internal/domain/identity"
)

var (
	ErrNotFound = identity.ErrStoreNotFound
	ErrConflict = identity.ErrStoreConflict
)

// Store is the combined identity persistence layer.
type Store = identity.Store
type UserStore = identity.UserStore
type PasskeyStore = identity.PasskeyStore
type RecoveryStore = identity.RecoveryStore
type WebSessionStore = identity.WebSessionStore
