// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package model

// LifecycleState represents the internal domain-level state of a stream session.
// It is decoupled from the HTTP DTOs to maintain clean layering.
type LifecycleState string

const (
	LifecycleStarting  LifecycleState = "starting"
	LifecycleBuffering LifecycleState = "buffering"
	LifecycleActive    LifecycleState = "active"
	LifecycleStalled   LifecycleState = "stalled"
	LifecycleEnding    LifecycleState = "ending"
	LifecycleIdle      LifecycleState = "idle"
	LifecycleError     LifecycleState = "error"
)
