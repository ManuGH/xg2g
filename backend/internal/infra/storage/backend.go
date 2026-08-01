// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

var (
	ErrObjectNotFound = errors.New("storage object not found")
)

// StorageType indicates the underlying storage engine mechanism.
type StorageType string

const (
	StorageTypeLocal StorageType = "LOCAL"
	StorageTypeNFS   StorageType = "NFS"
	StorageTypeCIFS  StorageType = "CIFS"
	StorageTypeS3    StorageType = "S3"
)

// StorageRole defines the operational purpose of a storage backend.
type StorageRole string

const (
	RoleRetroDVR        StorageRole = "RETRO_DVR"
	RoleStaging         StorageRole = "STAGING"
	RoleRecordingTarget StorageRole = "RECORDING_TARGET"
	RoleArchive         StorageRole = "ARCHIVE"
)

// HealthState indicates operational readiness.
type HealthState string

const (
	HealthStateHealthy  HealthState = "HEALTHY"
	HealthStateDegraded HealthState = "DEGRADED"
	HealthStateOffline  HealthState = "OFFLINE"
)

// HealthStatus details current backend availability.
type HealthStatus struct {
	State        HealthState `json:"state"`
	Readable     bool        `json:"readable"`
	Writable     bool        `json:"writable"`
	ErrorMessage string      `json:"error_message,omitempty"`
	CheckedAt    time.Time   `json:"checked_at"`
}

// CapacityInfo details total, used, and available bytes on a storage target.
type CapacityInfo struct {
	TotalBytes     int64 `json:"total_bytes"`
	UsedBytes      int64 `json:"used_bytes"`
	AvailableBytes int64 `json:"available_bytes"`
}

// ObjectInfo details metadata of a stored object.
type ObjectInfo struct {
	ObjectKey string    `json:"object_key"`
	SizeBytes int64     `json:"size_bytes"`
	UpdatedAt time.Time `json:"updated_at"`
}

// StorageCapabilities describes supported filesystem and I/O features.
type StorageCapabilities struct {
	SupportsHardlink         bool `json:"supports_hardlink"`
	SupportsReflink          bool `json:"supports_reflink"`
	SupportsAtomicRename     bool `json:"supports_atomic_rename"`
	SupportsAtomicReplace    bool `json:"supports_atomic_replace"`
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
	CommitFile(ctx context.Context, srcLocalPath string, targetObjectKey string) error
	Stat(ctx context.Context, objectKey string) (ObjectInfo, error)
	DeleteFile(ctx context.Context, targetObjectKey string) error
}
