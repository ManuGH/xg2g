package model

// Recording represents a VOD item in the V3 system.
// This is a minimal representation for use in store abstraction and testing.
type Recording struct {
	ID                  string `json:"id"`
	Title               string `json:"title"`
	Description         string `json:"description"`
	ExtendedDescription string `json:"extendedDescription,omitempty"`
	ServiceRef          string `json:"serviceRef"`
	Begin               int64  `json:"begin"`
	Length              string `json:"length"` // e.g. "90 min"
	Filename            string `json:"filename"`
}
