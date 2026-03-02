// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lifecycle

import (
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

// NewSessionRecord initializes a session record with canonical lifecycle defaults.
func NewSessionRecord(now time.Time) *model.SessionRecord {
	return &model.SessionRecord{
		State:         model.SessionNew,
		CreatedAtUnix: now.Unix(),
		UpdatedAtUnix: now.Unix(),
	}
}
