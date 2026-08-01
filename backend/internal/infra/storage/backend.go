// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package storage

import (
	"context"
	"io"
	"time"
)

// StorageType identifies the physical or protocol type of a storage backend.
type StorageType string

const (
	StorageTypeLocal  StorageType = "LOCAL"
	StorageTypeUSB    StorageType = "USB"
	StorageTypeNFS    StorageType = "NFS"
	StorageTypeCIFS   StorageType = "CIFS"
	StorageTypeS3     StorageType = "S3"
	StorageTypeWebDAV StorageType = "WEBDAV"
)

// StorageRole identifies the functional role assigned to a storage backend.
type StorageRole string

const (
	RoleRetroDVR        StorageRole = "RETRO_DVR"
	RoleStaging         StorageRole = "STAGING"
	RoleRecordingTarget StorageRole = "RECORDING_TARGET"
	RoleArchiveTarget   StorageRole = "ARCHIVE_TARGET"
)

// HealthState represents the operational status of a storage target.
type HealthState string

const (
	HealthStateHealthy     HealthState = "HEALTHY"
	HealthStateDegraded    HealthState = "DEGRADED"
	HealthStateUnavailable HealthState = "UNAVAILABLE"
)

// HealthStatus details the current health check result of a storage backend.
type HealthStatus struct {
	State        HealthState `json:"state"`
	Readable     bool        `json:"readable"`
	Writable     bool        `json:"writable"`
	LatencyMs    int64       `json:"latency_ms"`
	LastCheckTime time.Time   `json:"last_check_time"`
	LastError    string      `json:"last_error,omitempty"`
}

// CapacityInfo details total, used, and available bytes on a storage target.
type CapacityInfo struct {
	TotalBytes     int64 `json:"total_bytes"`
	UsedBytes      int64 `json:"used_bytes"`
	AvailableBytes int64 `json:"available_bytes"`
}

// StorageCapabilities describes supported filesystem and I/O features.
type StorageCapabilities struct {
	SupportsHardlink         bool `json:"supports_hardlink"`
	SupportsReflink          bool `json:"supports_reflink"`
	SupportsAtomicRename     bool `json:"supports_atomic_rename"`
	RecommendedForRingbuffer bool `json:"recommended_for_ringbuffer"`
}

// ObjectReader represents a full seekable media object stream for local or mounted storage targets.
type ObjectReader interface {
	io.Reader
	io.ReaderAt
	io.Seeker
	io.Closer
	Size() int64
}

// StorageBackend defines the capability-based interface for all xg2g storage targets.
type StorageBackend interface {
	ID() string
	Type() StorageType
	Roles() []StorageRole
	Capabilities() StorageCapabilities
	Health(ctx context.Context) HealthStatus
	Capacity(ctx context.Context) (CapacityInfo, error)
	Open(ctx context.Context, objectKey string) (ObjectReader, error)
	OpenRange(ctx context.Context, objectKey string, offset, length int64) (io.ReadCloser, error)
}
