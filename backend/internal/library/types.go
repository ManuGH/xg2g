// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Package library implements filesystem-based media library indexing per ADR-ENG-002.
// This layer is receiver-independent and provides stable media access even when
// Enigma2 is unavailable.
package library

import (
	"fmt"
	"time"
)

// RootStatus represents the runtime state of a library root.
// Per P0+ Gate #3: Status enum is never|running|ok|degraded|failed.
type RootStatus string

const (
	RootStatusNever    RootStatus = "never"    // Not yet scanned
	RootStatusRunning  RootStatus = "running"  // Scan in progress
	RootStatusOK       RootStatus = "ok"       // Last scan successful
	RootStatusDegraded RootStatus = "degraded" // Last scan had partial errors
	RootStatusFailed   RootStatus = "failed"   // Last scan failed completely
)

// String returns the string representation of RootStatus.
func (r RootStatus) String() string {
	return string(r)
}

// RootConfig represents the configuration for a library root.
// Maps directly from config.yaml library.roots[].
type RootConfig struct {
	ID         string   // Unique identifier (user-defined)
	Path       string   // Absolute path on host (NOT exposed in API per P0+ Gate #1)
	Type       string   // smb|nfs|local (label only, no mount management)
	MaxDepth   int      // Maximum directory scan depth
	IncludeExt []string // File extensions to include (e.g., [".ts", ".mp4"])
}

// Root represents a library root with runtime status.
// This is the API representation (path excluded per ADR-SEC-001).
type Root struct {
	ID             string     `json:"id"`
	Type           string     `json:"type"`
	LastScanTime   *time.Time `json:"last_scan_time,omitempty"`
	LastScanStatus RootStatus `json:"last_scan_status"`
	TotalItems     int        `json:"total_items"`
}

// ItemStatus represents the status of a single library item.
// Per P0+ Gate #3: Uses ok|unreadable (NOT unavailable - that's root-level).
type ItemStatus string

const (
	ItemStatusOK         ItemStatus = "ok"         // File readable and valid
	ItemStatusUnreadable ItemStatus = "unreadable" // File exists but cannot be read
)

// String returns the string representation of ItemStatus.
func (i ItemStatus) String() string {
	return string(i)
}

// Item represents a single media file in the library.
type Item struct {
	RootID          string     `json:"root_id"`
	RelPath         string     `json:"rel_path"` // Relative to root (safe to expose in API)
	Filename        string     `json:"filename"`
	SizeBytes       int64      `json:"size_bytes"`
	ModTime         time.Time  `json:"mod_time"`
	ScanTime        time.Time  `json:"scan_time"`
	DurationSeconds int64      `json:"duration_seconds"`
	Status          ItemStatus `json:"status"`
}

// ScanResult represents the outcome of a library root scan.
type ScanResult struct {
	RootID        string
	Started       time.Time
	Finished      time.Time
	TotalScanned  int // Files encountered
	ItemsInserted int // New items added
	ItemsUpdated  int // Existing items updated
	ItemsSkipped  int // Files skipped (wrong ext, depth, etc.)
	ErrorCount    int // Errors encountered (permission denied, etc.)
	FinalStatus   RootStatus
	LastError     string // Last error message (for diagnostics)
}

// Error returns a human-readable error summary if the scan had issues.
func (s *ScanResult) Error() string {
	if s.ErrorCount == 0 && s.FinalStatus == RootStatusOK {
		return ""
	}
	return fmt.Sprintf("scan completed with %d errors, status=%s", s.ErrorCount, s.FinalStatus)
}
