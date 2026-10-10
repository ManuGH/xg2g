// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package relay

import (
	"errors"
	"time"
)

var (
	// ErrRecursiveRelayTarget is returned when an upstream target points back to xg2g itself.
	ErrRecursiveRelayTarget = errors.New("upstream target points to xg2g; recursive relaying prohibited")
	// ErrRelayUnauthorized is returned when the private relay rejects the request authentication.
	ErrRelayUnauthorized = errors.New("private relay authentication failed")
	// ErrRelayUnavailable is returned when the private relay service is unreachable or returns a 5xx error.
	ErrRelayUnavailable = errors.New("private box relay unreachable or unavailable")
	// ErrMissingSlotLease is returned when attempting to fetch media without a valid admission slot lease.
	ErrMissingSlotLease = errors.New("valid slot lease required to fetch from private relay")
	// ErrInvalidRelayConfig is returned when relay client configuration is missing mandatory fields.
	ErrInvalidRelayConfig = errors.New("invalid private relay configuration")
)

// ClientConfig holds configuration for communicating with the private box relay.
type ClientConfig struct {
	RelayBaseURL          string        // Base URL of the private relay (e.g. http://10.10.55.64:8085)
	AuthToken             string        // Secret authentication token for relay authorization
	SelfAddresses         []string      // Hostnames/IPs belonging to xg2g (used to detect and prevent loops)
	ConnectTimeout        time.Duration // Connection establishment timeout
	ResponseHeaderTimeout time.Duration // Maximum wait for the relay to begin the stream; does not cap stream duration
}
