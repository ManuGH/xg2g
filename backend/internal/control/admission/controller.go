package admission

import (
	"context"

	"github.com/ManuGH/xg2g/internal/config"
)

// Decision represents the outcome of an admission check.
type Decision struct {
	Allow             bool
	Problem           *Problem
	RetryAfterSeconds *int
}

// Request represents the input needed for an admission decision.
// Currently minimal, but extensible for future features (e.g. prioritized requests).
type Request struct {
	WantsTranscode bool
}

// RuntimeState encapsulates all necessary runtime metrics.
// This struct must be populated by the caller from live system state.
type RuntimeState struct {
	TunerSlots       int
	SessionsActive   int
	TranscodesActive int
}

// CapacityController abstracts the admission logic.
type CapacityController interface {
	Check(ctx context.Context, req Request, state RuntimeState) Decision
}

// Controller implements CapacityController with deterministic rules.
type Controller struct {
	cfg config.AppConfig
}

// NewController creates a new admission controller with the given configuration.
func NewController(cfg config.AppConfig) *Controller {
	return &Controller{
		cfg: cfg,
	}
}

// Check evaluates whether a request should be admitted based on configuration and runtime state.
//
// Rules (Strict Order):
// 1. Engine disabled -> Reject
// 2. Monitoring State Invalid -> Reject (Fail Closed)
// 3. No tuner slots -> Reject
// 4. MaxSessions exceeded -> Reject
// 5. MaxTranscodes exceeded -> Reject
// 6. Allow
func (c *Controller) Check(ctx context.Context, req Request, state RuntimeState) Decision {
	// Rule 1: Engine Disabled
	if !c.cfg.Engine.Enabled {
		return Decision{
			Allow:   false,
			Problem: NewEngineDisabled(),
		}
	}

	// Rule 2: Fail Closed on Invalid State
	// If metrics are negative, something is wrong with the monitoring system.
	if state.TunerSlots < 0 || state.SessionsActive < 0 || state.TranscodesActive < 0 {
		return Decision{
			Allow:   false,
			Problem: NewStateUnknown(),
		}
	}

	// Rule 3: Tuner Slots
	// If no tuner slots are available (and not virtual mode 0-override), streaming is impossible.
	// Note: config.AppConfig is source of truth for overrides, but runtime state reflects reality.
	// If config baselines provided >0 slots, but state says 0 (e.g. discovery failed), we trust state?
	// The prompt Says: "TunerSlots []int (runtime-discovered at bootstrap; not from config unless manual override)"
	// Implementation: The caller passes `state.TunerSlots` which is len(cfg.Engine.TunerSlots).
	if state.TunerSlots <= 0 {
		return Decision{
			Allow:   false,
			Problem: NewNoTuners(state.TunerSlots),
		}
	}

	// Rule 4: Max Sessions
	// Limit 0 means "disabled" or "unlimited"?
	// Prompt: "MaxSessions exceeded -> reject".
	// config defaults are 8.
	// If limit is <= 0, we treat it as disabled/strict check?
	// User said: "If cfg.Limits.MaxSessions < 1 ... that is config validation job".
	// So we assume limit >= 1.
	if state.SessionsActive >= c.cfg.Limits.MaxSessions {
		return Decision{
			Allow:   false,
			Problem: NewSessionsFull(state.SessionsActive, c.cfg.Limits.MaxSessions),
		}
	}

	// Rule 5: Max Transcodes
	if req.WantsTranscode && state.TranscodesActive >= c.cfg.Limits.MaxTranscodes {
		return Decision{
			Allow:   false,
			Problem: NewTranscodesFull(state.TranscodesActive, c.cfg.Limits.MaxTranscodes),
		}
	}

	return Decision{Allow: true}
}
