// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// PersistentSagaStore is a production-grade, SQLite-backed implementation of SagaStore with ACID transactions.
type PersistentSagaStore struct {
	db *sql.DB
	mu sync.Mutex // guards transaction serialization on SQLite connections
}

// NewPersistentSagaStore initializes a new SQLite-backed PersistentSagaStore at the given dbPath.
func NewPersistentSagaStore(dbPath string) (*PersistentSagaStore, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, fmt.Errorf("dbPath cannot be empty")
	}
	if dbPath != ":memory:" {
		dir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dir, 0750); err != nil {
			return nil, fmt.Errorf("failed to create database directory '%s': %w", dir, err)
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db at '%s': %w", dbPath, err)
	}

	// SQLite tuning for robust embedded concurrency
	db.SetMaxOpenConns(1)

	store := &PersistentSagaStore{db: db}
	if err := store.initSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to initialize sqlite schema: %w", err)
	}

	return store, nil
}

// Close closes the database connection.
func (p *PersistentSagaStore) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.db.Close()
}

func (p *PersistentSagaStore) initSchema(ctx context.Context) error {
	schema := `
	CREATE TABLE IF NOT EXISTS preemption_sagas (
		saga_id TEXT PRIMARY KEY,
		receiver_id TEXT NOT NULL,
		contract_id TEXT NOT NULL,
		prepared_teardown_hash TEXT NOT NULL,
		state TEXT NOT NULL,
		mode TEXT NOT NULL,
		version INTEGER NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		expires_at TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS receiver_claims (
		receiver_id TEXT PRIMARY KEY,
		saga_id TEXT NOT NULL,
		status TEXT NOT NULL,
		lease_until TEXT NOT NULL,
		claimed_at TEXT NOT NULL,
		claim_version INTEGER NOT NULL,
		fencing_token INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS preemption_saga_transitions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		saga_id TEXT NOT NULL,
		sequence INTEGER NOT NULL,
		from_state TEXT NOT NULL,
		to_state TEXT NOT NULL,
		reason_code TEXT NOT NULL,
		occurred_at TEXT NOT NULL,
		actor TEXT NOT NULL,
		details TEXT NOT NULL,
		FOREIGN KEY(saga_id) REFERENCES preemption_sagas(saga_id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_sagas_prepared_hash ON preemption_sagas(prepared_teardown_hash);
	CREATE INDEX IF NOT EXISTS idx_transitions_saga ON preemption_saga_transitions(saga_id, sequence);
	`

	p.mu.Lock()
	defer p.mu.Unlock()

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return err
	}

	return tx.Commit()
}

func (p *PersistentSagaStore) CreateSaga(ctx context.Context, record PreemptionSagaRecord) (PreemptionSagaRecord, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return PreemptionSagaRecord{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	// 1. Check existing SagaID
	var existingRec PreemptionSagaRecord
	existingRec, err = p.getSagaTx(ctx, tx, record.SagaID)
	if err == nil {
		if existingRec.ContractID == record.ContractID &&
			existingRec.PreparedTeardownHash == record.PreparedTeardownHash &&
			existingRec.ReceiverID == record.ReceiverID &&
			existingRec.Mode == record.Mode &&
			existingRec.ExpiresAt.Equal(record.ExpiresAt) {
			return existingRec, false, nil
		}
		return PreemptionSagaRecord{}, false, fmt.Errorf("%w: saga '%s' exists with mismatched content", ErrSagaIdentityConflict, record.SagaID)
	} else if !errors.Is(err, ErrSagaNotFound) {
		return PreemptionSagaRecord{}, false, err
	}

	// 2. Check active saga for same PreparedTeardownHash
	rows, err := tx.QueryContext(ctx, "SELECT saga_id, state FROM preemption_sagas WHERE prepared_teardown_hash = ?", record.PreparedTeardownHash)
	if err != nil {
		return PreemptionSagaRecord{}, false, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var activeID, activeState string
		if err := rows.Scan(&activeID, &activeState); err != nil {
			return PreemptionSagaRecord{}, false, err
		}
		if !PreemptionSagaState(activeState).IsTerminal() {
			fullActive, getErr := p.getSagaTx(ctx, tx, activeID)
			if getErr != nil {
				return PreemptionSagaRecord{}, false, getErr
			}
			return fullActive, false, ErrDuplicateSaga
		}
	}

	if record.Version == 0 {
		record.Version = 1
	}

	// Insert saga
	insertQuery := `
	INSERT INTO preemption_sagas (saga_id, receiver_id, contract_id, prepared_teardown_hash, state, mode, version, created_at, updated_at, expires_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`
	_, err = tx.ExecContext(ctx, insertQuery,
		record.SagaID,
		record.ReceiverID,
		record.ContractID,
		record.PreparedTeardownHash,
		string(record.State),
		string(record.Mode),
		record.Version,
		record.CreatedAt.Format(time.RFC3339Nano),
		record.UpdatedAt.Format(time.RFC3339Nano),
		record.ExpiresAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return PreemptionSagaRecord{}, false, err
	}

	for _, t := range record.History {
		insertTransQuery := `
		INSERT INTO preemption_saga_transitions (saga_id, sequence, from_state, to_state, reason_code, occurred_at, actor, details)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?);
		`
		_, err = tx.ExecContext(ctx, insertTransQuery,
			record.SagaID, t.Sequence, string(t.From), string(t.To),
			string(t.ReasonCode), t.OccurredAt.Format(time.RFC3339Nano), t.Actor, t.Details)
		if err != nil {
			return PreemptionSagaRecord{}, false, err
		}
	}

	if err := tx.Commit(); err != nil {
		return PreemptionSagaRecord{}, false, err
	}

	return record, true, nil
}

func (p *PersistentSagaStore) GetSaga(ctx context.Context, sagaID string) (PreemptionSagaRecord, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return PreemptionSagaRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()

	rec, err := p.getSagaTx(ctx, tx, sagaID)
	if err != nil {
		return PreemptionSagaRecord{}, err
	}
	return rec, nil
}

func (p *PersistentSagaStore) ClaimSagaExecution(
	ctx context.Context,
	sagaID string,
	expectedVersion uint64,
	receiverID string,
	now time.Time,
	leaseUntil time.Time,
	transition SagaTransition,
) (PreemptionSagaRecord, ReceiverClaim, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return PreemptionSagaRecord{}, ReceiverClaim{}, err
	}
	defer func() { _ = tx.Rollback() }()

	saga, err := p.getSagaTx(ctx, tx, sagaID)
	if err != nil {
		return PreemptionSagaRecord{}, ReceiverClaim{}, err
	}

	if saga.ReceiverID != receiverID {
		return PreemptionSagaRecord{}, ReceiverClaim{}, fmt.Errorf("%w: receiver '%s' != saga receiver '%s'", ErrFencingTokenMismatch, receiverID, saga.ReceiverID)
	}

	if saga.Version != expectedVersion {
		return PreemptionSagaRecord{}, ReceiverClaim{}, fmt.Errorf("%w: saga version %d != expected %d", ErrSagaVersionConflict, saga.Version, expectedVersion)
	}

	if saga.State != StatePrepared {
		return PreemptionSagaRecord{}, ReceiverClaim{}, fmt.Errorf("%w: cannot claim execution from state %s", ErrInvalidStateTransition, saga.State)
	}

	// Read current claim for receiver
	var currentStatus, currentSagaID, currentLeaseStr string
	var currentClaimVer, currentFencingToken uint64
	claimExists := true

	claimQuery := "SELECT saga_id, status, lease_until, claim_version, fencing_token FROM receiver_claims WHERE receiver_id = ?"
	err = tx.QueryRowContext(ctx, claimQuery, receiverID).Scan(&currentSagaID, &currentStatus, &currentLeaseStr, &currentClaimVer, &currentFencingToken)
	if err == sql.ErrNoRows {
		claimExists = false
	} else if err != nil {
		return PreemptionSagaRecord{}, ReceiverClaim{}, err
	}

	if claimExists {
		if ReceiverClaimStatus(currentStatus) == ClaimStatusQuarantined {
			return PreemptionSagaRecord{}, ReceiverClaim{}, fmt.Errorf("%w: receiver '%s' is quarantined", ErrReceiverClaimLocked, receiverID)
		}
		if ReceiverClaimStatus(currentStatus) == ClaimStatusClaimed && currentSagaID != sagaID {
			curLease, _ := time.Parse(time.RFC3339Nano, currentLeaseStr)
			if !now.After(curLease) {
				return PreemptionSagaRecord{}, ReceiverClaim{}, fmt.Errorf("%w: receiver '%s' claimed by saga '%s' until %s", ErrReceiverClaimLocked, receiverID, currentSagaID, curLease)
			}
		}
	}

	newFencingToken := uint64(1)
	newClaimVersion := uint64(1)
	if claimExists {
		newFencingToken = currentFencingToken + 1
		newClaimVersion = currentClaimVer + 1
	}

	newClaim := ReceiverClaim{
		ReceiverID:   receiverID,
		SagaID:       sagaID,
		Status:       ClaimStatusClaimed,
		LeaseUntil:   leaseUntil,
		ClaimedAt:    now,
		ClaimVersion: newClaimVersion,
		FencingToken: newFencingToken,
	}

	// Upsert receiver_claims
	upsertClaimQuery := `
	INSERT INTO receiver_claims (receiver_id, saga_id, status, lease_until, claimed_at, claim_version, fencing_token)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(receiver_id) DO UPDATE SET
		saga_id = excluded.saga_id,
		status = excluded.status,
		lease_until = excluded.lease_until,
		claimed_at = excluded.claimed_at,
		claim_version = excluded.claim_version,
		fencing_token = excluded.fencing_token;
	`
	_, err = tx.ExecContext(ctx, upsertClaimQuery,
		receiverID,
		sagaID,
		string(ClaimStatusClaimed),
		leaseUntil.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
		newClaimVersion,
		newFencingToken,
	)
	if err != nil {
		return PreemptionSagaRecord{}, ReceiverClaim{}, err
	}

	// Update saga (save original state for history!)
	originalState := saga.State
	saga.State = StateExecutionClaimed
	saga.Version++
	saga.UpdatedAt = now
	transition.Sequence = nextSequence(saga.History)
	transition.From = originalState
	transition.To = StateExecutionClaimed
	transition.OccurredAt = now

	_, err = tx.ExecContext(ctx, "UPDATE preemption_sagas SET state = ?, version = ?, updated_at = ? WHERE saga_id = ?",
		string(StateExecutionClaimed), saga.Version, now.Format(time.RFC3339Nano), sagaID)
	if err != nil {
		return PreemptionSagaRecord{}, ReceiverClaim{}, err
	}

	insertTransQuery := `
	INSERT INTO preemption_saga_transitions (saga_id, sequence, from_state, to_state, reason_code, occurred_at, actor, details)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?);
	`
	_, err = tx.ExecContext(ctx, insertTransQuery,
		sagaID, transition.Sequence, string(transition.From), string(transition.To),
		string(transition.ReasonCode), transition.OccurredAt.Format(time.RFC3339Nano), transition.Actor, transition.Details)
	if err != nil {
		return PreemptionSagaRecord{}, ReceiverClaim{}, err
	}

	saga.History = append(saga.History, transition)

	if err := tx.Commit(); err != nil {
		return PreemptionSagaRecord{}, ReceiverClaim{}, err
	}

	return saga, newClaim, nil
}

func (p *PersistentSagaStore) CompareAndSwapState(
	ctx context.Context,
	sagaID string,
	expectedVersion uint64,
	receiverID string,
	fencingToken uint64,
	fromState PreemptionSagaState,
	toState PreemptionSagaState,
	transition SagaTransition,
) (PreemptionSagaRecord, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if toState == StateTeardownRequested || toState == StateTargetStopped || toState == StateResourcesReleased || toState == StateRequesterStarted || toState == StateCompleted {
		return PreemptionSagaRecord{}, ErrMutationPhaseDisabled
	}

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return PreemptionSagaRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()

	saga, err := p.getSagaTx(ctx, tx, sagaID)
	if err != nil {
		return PreemptionSagaRecord{}, err
	}

	if saga.ReceiverID != receiverID {
		return PreemptionSagaRecord{}, fmt.Errorf("%w: receiver '%s' != saga receiver '%s'", ErrFencingTokenMismatch, receiverID, saga.ReceiverID)
	}

	if saga.Version != expectedVersion {
		return PreemptionSagaRecord{}, fmt.Errorf("%w: saga version %d != expected %d", ErrSagaVersionConflict, saga.Version, expectedVersion)
	}
	if saga.State != fromState {
		return PreemptionSagaRecord{}, fmt.Errorf("%w: current state %s != expected %s", ErrInvalidStateTransition, saga.State, fromState)
	}

	// Verify receiver claim & fencing token strictly
	var currentSagaID, currentStatus string
	var currentToken uint64
	err = tx.QueryRowContext(ctx, "SELECT saga_id, status, fencing_token FROM receiver_claims WHERE receiver_id = ?", receiverID).Scan(&currentSagaID, &currentStatus, &currentToken)
	if err != nil || ReceiverClaimStatus(currentStatus) != ClaimStatusClaimed || currentSagaID != sagaID || currentToken != fencingToken {
		return PreemptionSagaRecord{}, fmt.Errorf("%w: claim mismatch for receiver '%s' (token %d)", ErrFencingTokenMismatch, receiverID, fencingToken)
	}

	originalState := saga.State
	saga.State = toState
	saga.Version++
	saga.UpdatedAt = transition.OccurredAt
	if saga.UpdatedAt.IsZero() {
		saga.UpdatedAt = time.Now().UTC()
	}

	_, err = tx.ExecContext(ctx, "UPDATE preemption_sagas SET state = ?, version = ?, updated_at = ? WHERE saga_id = ?",
		string(toState), saga.Version, saga.UpdatedAt.Format(time.RFC3339Nano), sagaID)
	if err != nil {
		return PreemptionSagaRecord{}, err
	}

	transition.Sequence = nextSequence(saga.History)
	transition.From = originalState
	transition.To = toState
	if transition.OccurredAt.IsZero() {
		transition.OccurredAt = saga.UpdatedAt
	}

	insertTransQuery := `
	INSERT INTO preemption_saga_transitions (saga_id, sequence, from_state, to_state, reason_code, occurred_at, actor, details)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?);
	`
	_, err = tx.ExecContext(ctx, insertTransQuery,
		sagaID, transition.Sequence, string(transition.From), string(transition.To),
		string(transition.ReasonCode), transition.OccurredAt.Format(time.RFC3339Nano), transition.Actor, transition.Details)
	if err != nil {
		return PreemptionSagaRecord{}, err
	}

	saga.History = append(saga.History, transition)

	if err := tx.Commit(); err != nil {
		return PreemptionSagaRecord{}, err
	}

	return saga, nil
}

func (p *PersistentSagaStore) RenewReceiverClaim(
	ctx context.Context,
	receiverID string,
	sagaID string,
	fencingToken uint64,
	expectedClaimVersion uint64,
	now time.Time,
	leaseUntil time.Time,
) (ReceiverClaim, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return ReceiverClaim{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var currentSagaID, currentStatus, currentLeaseStr, currentClaimedAtStr string
	var currentClaimVer, currentToken uint64

	err = tx.QueryRowContext(ctx, "SELECT saga_id, status, lease_until, claimed_at, claim_version, fencing_token FROM receiver_claims WHERE receiver_id = ?", receiverID).Scan(
		&currentSagaID, &currentStatus, &currentLeaseStr, &currentClaimedAtStr, &currentClaimVer, &currentToken)
	if err != nil || ReceiverClaimStatus(currentStatus) != ClaimStatusClaimed || currentSagaID != sagaID || currentToken != fencingToken {
		return ReceiverClaim{}, fmt.Errorf("%w: claim mismatch for receiver '%s'", ErrFencingTokenMismatch, receiverID)
	}

	if currentClaimVer != expectedClaimVersion {
		return ReceiverClaim{}, fmt.Errorf("%w: claim version %d != expected %d", ErrFencingTokenMismatch, currentClaimVer, expectedClaimVersion)
	}

	curLease, _ := time.Parse(time.RFC3339Nano, currentLeaseStr)
	if now.After(curLease) {
		return ReceiverClaim{}, fmt.Errorf("%w: claim has expired at %s", ErrFencingTokenMismatch, curLease)
	}

	newClaimVer := currentClaimVer + 1
	updateQuery := "UPDATE receiver_claims SET lease_until = ?, claim_version = ? WHERE receiver_id = ?"
	_, err = tx.ExecContext(ctx, updateQuery, leaseUntil.Format(time.RFC3339Nano), newClaimVer, receiverID)
	if err != nil {
		return ReceiverClaim{}, err
	}

	claimedAt, _ := time.Parse(time.RFC3339Nano, currentClaimedAtStr)

	if err := tx.Commit(); err != nil {
		return ReceiverClaim{}, err
	}

	return ReceiverClaim{
		ReceiverID:   receiverID,
		SagaID:       sagaID,
		Status:       ClaimStatusClaimed,
		LeaseUntil:   leaseUntil,
		ClaimedAt:    claimedAt,
		ClaimVersion: newClaimVer,
		FencingToken: fencingToken,
	}, nil
}

func (p *PersistentSagaStore) TransitionAndReleaseClaim(
	ctx context.Context,
	sagaID string,
	expectedVersion uint64,
	receiverID string,
	fencingToken uint64,
	toState PreemptionSagaState,
	transition SagaTransition,
) (PreemptionSagaRecord, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return PreemptionSagaRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()

	saga, err := p.getSagaTx(ctx, tx, sagaID)
	if err != nil {
		return PreemptionSagaRecord{}, err
	}

	if saga.ReceiverID != receiverID {
		return PreemptionSagaRecord{}, fmt.Errorf("%w: receiver '%s' != saga receiver '%s'", ErrFencingTokenMismatch, receiverID, saga.ReceiverID)
	}

	if saga.Version != expectedVersion {
		return PreemptionSagaRecord{}, fmt.Errorf("%w: saga version %d != expected %d", ErrSagaVersionConflict, saga.Version, expectedVersion)
	}

	// Strictly verify claim ownership, status, and fencing token! If mismatch -> ZERO MUTATIONS!
	var currentSagaID, currentStatus string
	var currentToken uint64
	err = tx.QueryRowContext(ctx, "SELECT saga_id, status, fencing_token FROM receiver_claims WHERE receiver_id = ?", receiverID).Scan(&currentSagaID, &currentStatus, &currentToken)
	if err != nil || ReceiverClaimStatus(currentStatus) != ClaimStatusClaimed || currentSagaID != sagaID || currentToken != fencingToken {
		return PreemptionSagaRecord{}, fmt.Errorf("%w: cannot release claim for receiver '%s' (fencing token mismatch)", ErrFencingTokenMismatch, receiverID)
	}

	// Release claim while preserving fencing_token counter
	_, err = tx.ExecContext(ctx, "UPDATE receiver_claims SET status = ?, saga_id = '' WHERE receiver_id = ?", string(ClaimStatusUnclaimed), receiverID)
	if err != nil {
		return PreemptionSagaRecord{}, err
	}

	originalState := saga.State
	saga.State = toState
	saga.Version++
	saga.UpdatedAt = transition.OccurredAt
	if saga.UpdatedAt.IsZero() {
		saga.UpdatedAt = time.Now().UTC()
	}

	_, err = tx.ExecContext(ctx, "UPDATE preemption_sagas SET state = ?, version = ?, updated_at = ? WHERE saga_id = ?",
		string(toState), saga.Version, saga.UpdatedAt.Format(time.RFC3339Nano), sagaID)
	if err != nil {
		return PreemptionSagaRecord{}, err
	}

	transition.Sequence = nextSequence(saga.History)
	transition.From = originalState
	transition.To = toState
	if transition.OccurredAt.IsZero() {
		transition.OccurredAt = saga.UpdatedAt
	}

	insertTransQuery := `
	INSERT INTO preemption_saga_transitions (saga_id, sequence, from_state, to_state, reason_code, occurred_at, actor, details)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?);
	`
	_, err = tx.ExecContext(ctx, insertTransQuery,
		sagaID, transition.Sequence, string(transition.From), string(transition.To),
		string(transition.ReasonCode), transition.OccurredAt.Format(time.RFC3339Nano), transition.Actor, transition.Details)
	if err != nil {
		return PreemptionSagaRecord{}, err
	}

	saga.History = append(saga.History, transition)

	if err := tx.Commit(); err != nil {
		return PreemptionSagaRecord{}, err
	}

	return saga, nil
}

func (p *PersistentSagaStore) QuarantineReceiverClaim(
	ctx context.Context,
	receiverID string,
	sagaID string,
	fencingToken uint64,
	reason string,
	now time.Time,
) (ReceiverClaim, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return ReceiverClaim{}, err
	}
	defer func() { _ = tx.Rollback() }()

	saga, err := p.getSagaTx(ctx, tx, sagaID)
	if err != nil || saga.ReceiverID != receiverID {
		return ReceiverClaim{}, fmt.Errorf("%w: saga '%s' mismatch for receiver '%s'", ErrSagaNotFound, sagaID, receiverID)
	}

	var currentSagaID, currentStatus string
	var currentToken uint64
	err = tx.QueryRowContext(ctx, "SELECT saga_id, status, fencing_token FROM receiver_claims WHERE receiver_id = ?", receiverID).Scan(&currentSagaID, &currentStatus, &currentToken)
	if err != nil || ReceiverClaimStatus(currentStatus) != ClaimStatusClaimed || currentSagaID != sagaID || currentToken != fencingToken {
		return ReceiverClaim{}, fmt.Errorf("%w: claim mismatch for receiver '%s'", ErrFencingTokenMismatch, receiverID)
	}

	updateQuery := "UPDATE receiver_claims SET status = ?, claimed_at = ?, claim_version = claim_version + 1 WHERE receiver_id = ?"
	_, err = tx.ExecContext(ctx, updateQuery, string(ClaimStatusQuarantined), now.Format(time.RFC3339Nano), receiverID)
	if err != nil {
		return ReceiverClaim{}, err
	}

	if err := tx.Commit(); err != nil {
		return ReceiverClaim{}, err
	}

	return ReceiverClaim{
		ReceiverID:   receiverID,
		SagaID:       sagaID,
		Status:       ClaimStatusQuarantined,
		ClaimedAt:    now,
		FencingToken: currentToken,
	}, nil
}

func (p *PersistentSagaStore) FindActiveByPreparedHash(ctx context.Context, preparedHash string) (PreemptionSagaRecord, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return PreemptionSagaRecord{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, "SELECT saga_id, state FROM preemption_sagas WHERE prepared_teardown_hash = ?", preparedHash)
	if err != nil {
		return PreemptionSagaRecord{}, false, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var sagaID, stateStr string
		if err := rows.Scan(&sagaID, &stateStr); err != nil {
			return PreemptionSagaRecord{}, false, err
		}
		if !PreemptionSagaState(stateStr).IsTerminal() {
			saga, err := p.getSagaTx(ctx, tx, sagaID)
			if err != nil {
				return PreemptionSagaRecord{}, false, err
			}
			return saga, true, nil
		}
	}

	return PreemptionSagaRecord{}, false, nil
}

func (p *PersistentSagaStore) GetReceiverClaim(ctx context.Context, receiverID string) (ReceiverClaim, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return ReceiverClaim{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var sagaID, status, leaseStr, claimedAtStr string
	var claimVer, fencingToken uint64

	err = tx.QueryRowContext(ctx, "SELECT saga_id, status, lease_until, claimed_at, claim_version, fencing_token FROM receiver_claims WHERE receiver_id = ?", receiverID).Scan(
		&sagaID, &status, &leaseStr, &claimedAtStr, &claimVer, &fencingToken)
	if err == sql.ErrNoRows {
		return ReceiverClaim{ReceiverID: receiverID, Status: ClaimStatusUnclaimed}, nil
	} else if err != nil {
		return ReceiverClaim{}, err
	}

	leaseUntil, _ := time.Parse(time.RFC3339Nano, leaseStr)
	claimedAt, _ := time.Parse(time.RFC3339Nano, claimedAtStr)

	return ReceiverClaim{
		ReceiverID:   receiverID,
		SagaID:       sagaID,
		Status:       ReceiverClaimStatus(status),
		LeaseUntil:   leaseUntil,
		ClaimedAt:    claimedAt,
		ClaimVersion: claimVer,
		FencingToken: fencingToken,
	}, nil
}

func (p *PersistentSagaStore) ListNonTerminalSagas(ctx context.Context) ([]PreemptionSagaRecord, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, "SELECT saga_id, state FROM preemption_sagas")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []PreemptionSagaRecord
	for rows.Next() {
		var sagaID, stateStr string
		if err := rows.Scan(&sagaID, &stateStr); err != nil {
			return nil, err
		}
		if !PreemptionSagaState(stateStr).IsTerminal() {
			saga, err := p.getSagaTx(ctx, tx, sagaID)
			if err != nil {
				return nil, err
			}
			result = append(result, saga)
		}
	}

	return result, nil
}

func (p *PersistentSagaStore) getSagaTx(ctx context.Context, tx *sql.Tx, sagaID string) (PreemptionSagaRecord, error) {
	var rec PreemptionSagaRecord
	var stateStr, modeStr, createdStr, updatedStr, expiresStr string

	query := `
	SELECT saga_id, receiver_id, contract_id, prepared_teardown_hash, state, mode, version, created_at, updated_at, expires_at
	FROM preemption_sagas WHERE saga_id = ?;
	`
	err := tx.QueryRowContext(ctx, query, sagaID).Scan(
		&rec.SagaID, &rec.ReceiverID, &rec.ContractID, &rec.PreparedTeardownHash,
		&stateStr, &modeStr, &rec.Version, &createdStr, &updatedStr, &expiresStr,
	)
	if err == sql.ErrNoRows {
		return PreemptionSagaRecord{}, ErrSagaNotFound
	} else if err != nil {
		return PreemptionSagaRecord{}, err
	}

	rec.State = PreemptionSagaState(stateStr)
	rec.Mode = PreemptionMode(modeStr)
	rec.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
	rec.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedStr)
	rec.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expiresStr)

	// Fetch history
	transRows, err := tx.QueryContext(ctx, `
	SELECT sequence, from_state, to_state, reason_code, occurred_at, actor, details
	FROM preemption_saga_transitions WHERE saga_id = ? ORDER BY sequence ASC;
	`, sagaID)
	if err != nil {
		return PreemptionSagaRecord{}, err
	}
	defer func() { _ = transRows.Close() }()

	for transRows.Next() {
		var t SagaTransition
		var fromStr, toStr, reasonStr, occurredStr string
		if err := transRows.Scan(&t.Sequence, &fromStr, &toStr, &reasonStr, &occurredStr, &t.Actor, &t.Details); err != nil {
			return PreemptionSagaRecord{}, err
		}
		t.From = PreemptionSagaState(fromStr)
		t.To = PreemptionSagaState(toStr)
		t.ReasonCode = SagaReasonCode(reasonStr)
		t.OccurredAt, _ = time.Parse(time.RFC3339Nano, occurredStr)
		rec.History = append(rec.History, t)
	}

	return rec, nil
}
