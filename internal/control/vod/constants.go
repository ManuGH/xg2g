package vod

import "time"

// Supervision constants (Phase-9 invariants).
const (
	// HeartbeatTimeout is the maximum time allowed without progress events.
	HeartbeatTimeout = 15 * time.Second
	// BuildStartTimeout is the max time to wait for runner minimum artifacts.
	// Tuned for local FS; raise for slower or high-latency runner setups.
	BuildStartTimeout = 300 * time.Millisecond

	// StopGrace is the grace period given to Stop() before Kill().
	StopGrace = 2 * time.Second

	// KillDelay is the additional time to wait for process termination after Kill().
	KillDelay = 5 * time.Second
)
