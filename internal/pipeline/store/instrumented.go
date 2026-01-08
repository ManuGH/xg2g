// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"context"
	"time"

	"github.com/ManuGH/xg2g/internal/pipeline/model"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	storeOps = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "v3_store_ops_total",
			Help: "Total store operations",
		},
		[]string{"backend", "op", "result"}, // result=success/error
	)
	storeLat = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "v3_store_op_seconds",
			Help:    "Store operation latency",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"backend", "op"},
	)
)

// instrumentedStore wraps any StateStore to capture metrics.
type instrumentedStore struct {
	inner   StateStore
	backend string
}

func NewInstrumentedStore(inner StateStore, backend string) StateStore {
	return &instrumentedStore{inner: inner, backend: backend}
}

func (i *instrumentedStore) observe(op string, start time.Time, err error) {
	dur := time.Since(start).Seconds()
	res := "success"
	if err != nil {
		res = "error"
	}
	storeOps.WithLabelValues(i.backend, op, res).Inc()
	storeLat.WithLabelValues(i.backend, op).Observe(dur)
}

func (i *instrumentedStore) PutSession(ctx context.Context, s *model.SessionRecord) (err error) {
	start := time.Now()
	defer func() { i.observe("put_session", start, err) }()
	return i.inner.PutSession(ctx, s)
}

// Correct approach
func (i *instrumentedStore) PutSessionWithIdempotency(ctx context.Context, s *model.SessionRecord, key string, ttl time.Duration) (existingID string, exists bool, err error) {
	start := time.Now()
	defer func() { i.observe("put_session_idem", start, err) }()
	return i.inner.PutSessionWithIdempotency(ctx, s, key, ttl)
}

func (i *instrumentedStore) GetSession(ctx context.Context, id string) (rec *model.SessionRecord, err error) {
	start := time.Now()
	defer func() { i.observe("get_session", start, err) }()
	return i.inner.GetSession(ctx, id)
}

func (i *instrumentedStore) UpdateSession(ctx context.Context, id string, fn func(*model.SessionRecord) error) (rec *model.SessionRecord, err error) {
	start := time.Now()
	defer func() { i.observe("update_session", start, err) }()
	return i.inner.UpdateSession(ctx, id, fn)
}

func (i *instrumentedStore) ListSessions(ctx context.Context) (list []*model.SessionRecord, err error) {
	start := time.Now()
	defer func() { i.observe("list_sessions", start, err) }()
	return i.inner.ListSessions(ctx)
}

func (i *instrumentedStore) QuerySessions(ctx context.Context, filter SessionFilter) (list []*model.SessionRecord, err error) {
	start := time.Now()
	defer func() { i.observe("query_sessions", start, err) }()
	return i.inner.QuerySessions(ctx, filter)
}

func (i *instrumentedStore) ScanSessions(ctx context.Context, fn func(*model.SessionRecord) error) (err error) {
	start := time.Now()
	defer func() { i.observe("scan_sessions", start, err) }()
	return i.inner.ScanSessions(ctx, fn)
}

func (i *instrumentedStore) DeleteSession(ctx context.Context, id string) (err error) {
	start := time.Now()
	defer func() { i.observe("delete_session", start, err) }()
	return i.inner.DeleteSession(ctx, id)
}

func (i *instrumentedStore) PutIdempotency(ctx context.Context, key, sessionID string, ttl time.Duration) (err error) {
	start := time.Now()
	defer func() { i.observe("put_idem", start, err) }()
	return i.inner.PutIdempotency(ctx, key, sessionID, ttl)
}

func (i *instrumentedStore) GetIdempotency(ctx context.Context, key string) (sid string, ok bool, err error) {
	start := time.Now()
	defer func() { i.observe("get_idem", start, err) }()
	return i.inner.GetIdempotency(ctx, key)
}

func (i *instrumentedStore) TryAcquireLease(ctx context.Context, key, owner string, ttl time.Duration) (l Lease, ok bool, err error) {
	start := time.Now()
	defer func() { i.observe("try_lease", start, err) }()
	return i.inner.TryAcquireLease(ctx, key, owner, ttl)
}

func (i *instrumentedStore) RenewLease(ctx context.Context, key, owner string, ttl time.Duration) (l Lease, ok bool, err error) {
	start := time.Now()
	defer func() { i.observe("renew_lease", start, err) }()
	return i.inner.RenewLease(ctx, key, owner, ttl)
}

func (i *instrumentedStore) ReleaseLease(ctx context.Context, key, owner string) (err error) {
	start := time.Now()
	defer func() { i.observe("release_lease", start, err) }()
	return i.inner.ReleaseLease(ctx, key, owner)
}

func (i *instrumentedStore) DeleteAllLeases(ctx context.Context) (count int, err error) {
	start := time.Now()
	defer func() { i.observe("delete_all_leases", start, err) }()
	return i.inner.DeleteAllLeases(ctx)
}
