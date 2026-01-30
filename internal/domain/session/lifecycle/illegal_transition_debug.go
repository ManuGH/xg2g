// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

//go:build debug

package lifecycle

import (
	"fmt"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

func illegalTransition(rec *model.SessionRecord, from model.SessionState, ev EventKind, now time.Time) (Transition, error) {
	panic(fmt.Sprintf("illegal transition: %s + %v", from, ev))
}
