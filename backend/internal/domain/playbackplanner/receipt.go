package playbackplanner

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// PlanningReceipt is a domain entity representing a binding promise from the planner.
// Lifecycle: issued -> consumed(sessionID) -> expired.
type PlanningReceipt struct {
	ReceiptID      string
	EvidenceHash   string
	PlanHash       string
	IssuedAt       int64
	ExpiresAt      int64
	PlannerVersion string
	PolicyVersion  string
	PrincipalBind  string
	ServiceRefBind string
	ScopeBind      string
	LifecycleState string // "issued", "consumed", "expired"
	// ConsumedSessionID makes consumption idempotent. A retried intent may load
	// the same immutable plan again, but it must resolve to the same session.
	ConsumedSessionID string
}

// Hash returns a deterministic hash of the plan to be embedded in the receipt.
func (p PlaybackPlan) Hash() (string, error) {
	p = p.cloneNormalized()
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(b)
	return fmt.Sprintf("%x", hash), nil
}
