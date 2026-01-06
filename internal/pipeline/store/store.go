// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"context"
	"errors"
	"time"

	"github.com/ManuGH/xg2g/internal/pipeline/model"
)

var (
	ErrIdempotentReplay = errors.New("idempotent replay")
	ErrNotFound         = errors.New("not found")
)

// Lease is a single-writer lock for a (receiver, serviceKey) or similar key.
// The owner string should be stable for the lifetime of the worker instance.
type Lease interface {
	Key() string
	Owner() string
	ExpiresAt() time.Time
}

// StateStore is the system-of-record for v3 sessions and pipelines.
//
// Design intent:
// - All ingress paths are read-only or create intents.
// - All side-effects (tuning, ffmpeg, packaging) are performed by workers.
// - Single-writer leases prevent stampedes.
type StateStore interface {
	// --- Session CRUD ---
	PutSession(ctx context.Context, s *model.SessionRecord) error
	// PutSessionWithIdempotency writes a session and an idempotency key atomicity/transactionally.
	// If the idempotency key already exists, it returns the existing sessionID, exists=true, and no error.
	PutSessionWithIdempotency(ctx context.Context, s *model.SessionRecord, idemKey string, ttl time.Duration) (existingID string, exists bool, err error)
	// GetSession returns the session record. If not found, it returns (nil, nil).
	// Callers must check for nil record before using it.
	GetSession(ctx context.Context, id string) (*model.SessionRecord, error)
	UpdateSession(ctx context.Context, id string, fn func(*model.SessionRecord) error) (*model.SessionRecord, error)
	ListSessions(ctx context.Context) ([]*model.SessionRecord, error)
	// ScanSessions iterates over all sessions calling fn. Safest for large datasets.
	ScanSessions(ctx context.Context, fn func(*model.SessionRecord) error) error
	DeleteSession(ctx context.Context, id string) error

	// --- Pipeline CRUD ---
	PutPipeline(ctx context.Context, p *model.PipelineRecord) error
	GetPipeline(ctx context.Context, id string) (*model.PipelineRecord, error)
	UpdatePipeline(ctx context.Context, id string, fn func(*model.PipelineRecord) error) (*model.PipelineRecord, error)

	// --- Idempotency window (start intents) ---
	PutIdempotency(ctx context.Context, key, sessionID string, ttl time.Duration) error
	GetIdempotency(ctx context.Context, key string) (sessionID string, ok bool, err error)

	// --- Leases (single-writer) ---
	TryAcquireLease(ctx context.Context, key, owner string, ttl time.Duration) (Lease, bool, error)
	RenewLease(ctx context.Context, key, owner string, ttl time.Duration) (Lease, bool, error)
	ReleaseLease(ctx context.Context, key, owner string) error
	DeleteAllLeases(ctx context.Context) (int, error)
}
