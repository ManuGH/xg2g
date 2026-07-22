// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"github.com/ManuGH/xg2g/internal/control/http/v3/sessions"
	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

func buildSessionRuntimePolicyReplay(session *model.SessionRecord) *runtimepolicy.RuntimePolicyReplay {
	return sessions.BuildSessionRuntimePolicyReplay(session)
}

func sessionContextValue(session *model.SessionRecord, key string) string {
	return sessions.SessionContextValue(session, key)
}
