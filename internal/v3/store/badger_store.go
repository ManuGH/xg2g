//go:build v3
// +build v3

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ManuGH/xg2g/internal/v3/model"
	"github.com/dgraph-io/badger/v4"
)

// BadgerStore is a minimal MVP StateStore. It is intentionally conservative:
// - sessions: key = "sess:<id>" (JSON)
// - idempotency: key = "idem:<key>" (value=sessionID) with TTL
// - leases: key = "lease:<key>" (JSON) with TTL
//
// NOTE: This is not yet a full outbox-consistent store. Phase-5 work item.
type BadgerStore struct {
	db *badger.DB
}

func OpenBadgerStore(path string) (*BadgerStore, error) {
	opts := badger.DefaultOptions(path).WithLogger(nil)
	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}
	return &BadgerStore{db: db}, nil
}

func (s *BadgerStore) Close() error { return s.db.Close() }

func (s *BadgerStore) PutSession(ctx context.Context, rec *model.SessionRecord) error {
	key := []byte("sess:" + rec.SessionID)
	buf, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Set(key, buf)
	})
}

func (s *BadgerStore) UpdateSession(ctx context.Context, id string, fn func(*model.SessionRecord) error) (*model.SessionRecord, error) {
	key := []byte("sess:" + id)
	var out model.SessionRecord
	err := s.db.Update(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		if err := item.Value(func(val []byte) error {
			return json.Unmarshal(val, &out)
		}); err != nil {
			return err
		}
		if err := fn(&out); err != nil {
			return err
		}
		buf, err := json.Marshal(out)
		if err != nil {
			return err
		}
		return txn.Set(key, buf)
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *BadgerStore) PutPipeline(ctx context.Context, rec *model.PipelineRecord) error {
	key := []byte("pipe:" + rec.PipelineID)
	buf, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Set(key, buf)
	})
}

func (s *BadgerStore) GetPipeline(ctx context.Context, id string) (*model.PipelineRecord, error) {
	key := []byte("pipe:" + id)
	var out model.PipelineRecord
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &out)
		})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *BadgerStore) UpdatePipeline(ctx context.Context, id string, fn func(*model.PipelineRecord) error) (*model.PipelineRecord, error) {
	key := []byte("pipe:" + id)
	var out model.PipelineRecord
	err := s.db.Update(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		if err := item.Value(func(val []byte) error {
			return json.Unmarshal(val, &out)
		}); err != nil {
			return err
		}
		if err := fn(&out); err != nil {
			return err
		}
		buf, err := json.Marshal(out)
		if err != nil {
			return err
		}
		return txn.Set(key, buf)
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *BadgerStore) GetSession(ctx context.Context, sessionID string) (*model.SessionRecord, error) {
	key := []byte("sess:" + sessionID)
	var out model.SessionRecord
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &out)
		})
	})
	if err != nil {
		if err == badger.ErrKeyNotFound {
			return nil, nil // Not found, no error
		}
		return nil, err
	}
	return &out, nil
}

func (s *BadgerStore) DeleteSession(ctx context.Context, id string) error {
	key := []byte("sess:" + id)
	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(key)
	})
}

func (s *BadgerStore) PutSessionWithIdempotency(ctx context.Context, rec *model.SessionRecord, idemKey string, ttl time.Duration) error {
	sessKey := []byte("sess:" + rec.SessionID)
	sessBuf, err := json.Marshal(rec)
	if err != nil {
		return err
	}

	return s.db.Update(func(txn *badger.Txn) error {
		// Idempotency check
		if idemKey != "" {
			iKey := []byte("idem:" + idemKey)
			if _, err := txn.Get(iKey); err == nil {
				return ErrIdempotentReplay // Already exists
			} else if err != badger.ErrKeyNotFound {
				return err
			}
			// Write Idem
			entry := badger.NewEntry(iKey, []byte(rec.SessionID)).WithTTL(ttl)
			if err := txn.SetEntry(entry); err != nil {
				return err
			}
		}
		// Write Session
		return txn.Set(sessKey, sessBuf)
	})
}

func (s *BadgerStore) ListSessions(ctx context.Context) ([]*model.SessionRecord, error) {
	var list []*model.SessionRecord
	err := s.ScanSessions(ctx, func(r *model.SessionRecord) error {
		list = append(list, r)
		return nil
	})
	return list, err
}

func (s *BadgerStore) ScanSessions(ctx context.Context, fn func(*model.SessionRecord) error) error {
	prefix := []byte("sess:")
	return s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			item := it.Item()
			var rec model.SessionRecord
			if err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &rec)
			}); err != nil {
				continue
			}
			if err := fn(&rec); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *BadgerStore) PutIdempotency(ctx context.Context, idemKey, sessionID string, ttl time.Duration) error {
	key := []byte("idem:" + idemKey)
	entry := badger.NewEntry(key, []byte(sessionID)).WithTTL(ttl)
	return s.db.Update(func(txn *badger.Txn) error {
		return txn.SetEntry(entry)
	})
}

func (s *BadgerStore) GetIdempotency(ctx context.Context, idemKey string) (string, bool, error) {
	key := []byte("idem:" + idemKey)
	var v string
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			v = string(val)
			return nil
		})
	})
	if err != nil {
		if err == badger.ErrKeyNotFound {
			return "", false, nil
		}
		return "", false, err
	}
	return v, true, nil
}

type leaseEnvelope struct {
	Owner     string    `json:"owner"`
	LeaseKey  string    `json:"leaseKey"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (s *BadgerStore) TryAcquireLease(ctx context.Context, leaseKey, owner string, ttl time.Duration) (Lease, bool, error) {
	// MVP: optimistic create-only via Get+Set. Not linearizable under contention.
	// For MVP/canary, prefer MemoryStore leases and upgrade to proper CAS/txn guards in Phase 5.
	key := []byte("lease:" + leaseKey)
	now := time.Now()
	exp := now.Add(ttl)
	env := leaseEnvelope{Owner: owner, LeaseKey: leaseKey, ExpiresAt: exp}
	buf, _ := json.Marshal(env)

	err := s.db.Update(func(txn *badger.Txn) error {
		_, err := txn.Get(key)
		if err == nil {
			return fmt.Errorf("lease held")
		}
		if err != badger.ErrKeyNotFound {
			return err
		}
		entry := badger.NewEntry(key, buf).WithTTL(ttl)
		return txn.SetEntry(entry)
	})
	if err != nil {
		if err.Error() == "lease held" {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &badgerLease{s: s, leaseKey: leaseKey, owner: owner, ttl: ttl, expiresAt: exp}, true, nil
}

type badgerLease struct {
	s         *BadgerStore
	leaseKey  string
	owner     string
	ttl       time.Duration
	expiresAt time.Time
}

func (l *badgerLease) Key() string          { return l.leaseKey }
func (l *badgerLease) Owner() string        { return l.owner }
func (l *badgerLease) ExpiresAt() time.Time { return l.expiresAt }

func (s *BadgerStore) RenewLease(ctx context.Context, leaseKey, owner string, ttl time.Duration) (Lease, bool, error) {
	key := []byte("lease:" + leaseKey)
	exp := time.Now().Add(ttl)
	env := leaseEnvelope{Owner: owner, LeaseKey: leaseKey, ExpiresAt: exp}
	buf, _ := json.Marshal(env)
	entry := badger.NewEntry(key, buf).WithTTL(ttl)

	err := s.db.Update(func(txn *badger.Txn) error {
		// Verify owner
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		var current leaseEnvelope
		if err := item.Value(func(val []byte) error {
			return json.Unmarshal(val, &current)
		}); err != nil {
			return err
		}
		if current.Owner != owner {
			return fmt.Errorf("lease owned by other")
		}
		return txn.SetEntry(entry)
	})
	if err != nil {
		if err == badger.ErrKeyNotFound {
			return nil, false, nil
		}
		if err.Error() == "lease owned by other" {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &badgerLease{s: s, leaseKey: leaseKey, owner: owner, ttl: ttl, expiresAt: exp}, true, nil
}

func (s *BadgerStore) ReleaseLease(ctx context.Context, leaseKey, owner string) error {
	key := []byte("lease:" + leaseKey)
	return s.db.Update(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err == badger.ErrKeyNotFound {
			return nil
		}
		if err != nil {
			return err
		}
		var current leaseEnvelope
		if err := item.Value(func(val []byte) error {
			return json.Unmarshal(val, &current)
		}); err != nil {
			return err
		}
		if current.Owner == owner {
			return txn.Delete(key)
		}
		return nil
	})
}

func (s *BadgerStore) DeleteAllLeases(ctx context.Context) (int, error) {
	prefix := []byte("lease:")
	// Badger DropPrefix doesn't return count.
	// We could scan and count, but that defeats the optimization.
	// For MVP BadgerStore, we return 0. (BoltStore is primary)
	return 0, s.db.DropPrefix(prefix)
}

// Ensure interface compliance at compile time.
var _ StateStore = (*BadgerStore)(nil)
var _ Lease = (*badgerLease)(nil)

// Guardrail: BadgerStore leases are MVP-only.
var ErrBadgerLeaseMVP = fmt.Errorf("badger lease implementation is MVP-only")
