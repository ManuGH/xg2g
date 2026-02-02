package openwebif

import (
	"errors"
	"time"
)

// TimerChangeFlavor defines the exact parameter set used for /api/timerchange.
type TimerChangeFlavor string

const (
	TimerChangeFlavorUnknown TimerChangeFlavor = "unknown"
	TimerChangeFlavorA       TimerChangeFlavor = "flavor_A" // channel + change_*
	TimerChangeFlavorB       TimerChangeFlavor = "flavor_B" // sRef + strict parameters
)

// TimerChangeCap stores the detected capabilities for the /api/timerchange endpoint.
// Using value semantics for atomic storage.
type TimerChangeCap struct {
	Supported  bool
	Forbidden  bool
	Flavor     TimerChangeFlavor
	DetectedAt time.Time
}

// ErrTimerUpdatePartial indicates that an update failed in a way that risks data consistency.
// Specifically: The new timer was added, but the old timer could not be deleted.
// This is a critical error that should trigger alerts.
var ErrTimerUpdatePartial = errors.New("timer update partial failure: duplicate timer risk (add succeeded, delete failed)")
