// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package session

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// SessionKey identifies an upstream broadcast session uniquely.
// It is a comparable Go struct suitable directly as a map key.
type SessionKey struct {
	ReceiverHost string // Canonical normalized IP or lowercase hostname
	StreamPort   int    // Upstream stream port (default 8001)
	ServiceRef   string // Enigma2 1:0:... service reference
	Profile      string // Stream profile (e.g. "native", "direct", "smooth")
	SourceType   string // Source type (e.g. "enigma2", "satip", "tsfile")
}

// Canonicalize returns a normalized, canonical copy of the SessionKey.
func (k SessionKey) Canonicalize() SessionKey {
	host := strings.TrimSpace(strings.ToLower(k.ReceiverHost))
	// If host contains port, strip it
	if h, p, err := net.SplitHostPort(host); err == nil {
		host = h
		if portNum, pErr := strconv.Atoi(p); pErr == nil && portNum > 0 && k.StreamPort <= 0 {
			k.StreamPort = portNum
		}
	}
	if host == "" {
		host = "127.0.0.1"
	}

	port := k.StreamPort
	if port <= 0 {
		port = 8001
	}

	sref := strings.TrimSpace(k.ServiceRef)
	// Normalize trailing colons if any
	sref = strings.TrimRight(sref, ":")

	profile := strings.TrimSpace(strings.ToLower(k.Profile))
	if profile == "" {
		profile = "native"
	}

	sourceType := strings.TrimSpace(strings.ToLower(k.SourceType))
	if sourceType == "" {
		sourceType = "enigma2"
	}

	return SessionKey{
		ReceiverHost: host,
		StreamPort:   port,
		ServiceRef:   sref,
		Profile:      profile,
		SourceType:   sourceType,
	}
}

// String renders a human-readable identifier for the session key.
func (k SessionKey) String() string {
	c := k.Canonicalize()
	return fmt.Sprintf("%s://%s:%d/%s@%s", c.SourceType, c.ReceiverHost, c.StreamPort, c.ServiceRef, c.Profile)
}
