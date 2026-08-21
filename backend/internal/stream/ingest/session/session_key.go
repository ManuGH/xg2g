// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package session

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

var (
	ErrInvalidReceiverHost = errors.New("receiver host cannot be empty")
	ErrInvalidServiceRef   = errors.New("service reference cannot be empty")
)

// SessionKey identifies an upstream broadcast session uniquely.
// It is a comparable Go struct suitable directly as a map key.
type SessionKey struct {
	ReceiverHost  string // Canonical normalized IP or lowercase hostname
	StreamPort    int    // Upstream stream port (default 8001)
	ServiceRef    string // Enigma2 1:0:... service reference
	Profile       string // Stream profile (e.g. "native", "direct", "smooth")
	SourceType    string // Source type (e.g. "enigma2", "satip", "tsfile")
	TargetProgram uint16 // Optional target DVB program number (0 for auto/first)
}

// NewSessionKey creates a basic SessionKey from host, port, and serviceRef.
func NewSessionKey(receiverHost string, streamPort int, serviceRef string) SessionKey {
	if receiverHost == "" {
		receiverHost = "127.0.0.1"
	}
	if streamPort <= 0 {
		streamPort = 8001
	}
	return SessionKey{
		ReceiverHost: receiverHost,
		StreamPort:   streamPort,
		ServiceRef:   serviceRef,
		Profile:      "native",
		SourceType:   "enigma2",
	}
}

// Canonicalize returns a normalized copy of the SessionKey without modifying missing required fields.
func (k SessionKey) Canonicalize() SessionKey {
	host := strings.TrimSpace(strings.ToLower(k.ReceiverHost))
	port := k.StreamPort

	// If host contains port, split it
	if h, p, err := net.SplitHostPort(host); err == nil {
		host = h
		if portNum, pErr := strconv.Atoi(p); pErr == nil && portNum > 0 && port <= 0 {
			port = portNum
		}
	}

	if port <= 0 {
		port = 8001
	}

	sref := strings.TrimSpace(k.ServiceRef)
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
		ReceiverHost:  host,
		StreamPort:    port,
		ServiceRef:    sref,
		Profile:       profile,
		SourceType:    sourceType,
		TargetProgram: k.TargetProgram,
	}
}

// Validate checks whether the key contains all required fields.
func (k SessionKey) Validate() error {
	c := k.Canonicalize()
	if c.ReceiverHost == "" {
		return ErrInvalidReceiverHost
	}
	if c.ServiceRef == "" {
		return ErrInvalidServiceRef
	}
	return nil
}

// String renders a human-readable identifier for the session key.
func (k SessionKey) String() string {
	c := k.Canonicalize()
	return fmt.Sprintf("%s://%s:%d/%s@%s", c.SourceType, c.ReceiverHost, c.StreamPort, c.ServiceRef, c.Profile)
}
