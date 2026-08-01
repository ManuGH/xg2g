// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package recording

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	ErrTransferTaskNotFound      = errors.New("transfer task not found")
	ErrTransferTaskAlreadyExists = errors.New("transfer task already exists")
	ErrNoTransferTaskToClaim     = errors.New("no pending transfer task available for claiming")
	ErrWorkerLeaseLost           = errors.New("worker lease lost or task acquired by another worker")
	ErrTaskLeaseActive           = errors.New("cannot recover task state while worker lease is actively running")
)

// TransferState defines the lifecycle states of background transfer retries.
type TransferState string

const (
	TransferPending   TransferState = "PENDING"
	TransferRunning   TransferState = "RUNNING"
	TransferRetrying  TransferState = "RETRYING"
	TransferCompleted TransferState = "COMPLETED"
	TransferFailed    TransferState = "FAILED"
)

// TransferTask represents a persistent background transfer job for target backend commits.
type TransferTask struct {
	ID                string        `json:"id"`
	JobID             string        `json:"job_id"`
	AssetID           string        `json:"asset_id"`
	SourceWorkspaceID string        `json:"source_workspace_id"` // Matches JobID
	SourceFilename    string        `json:"source_filename"`    // Filename inside finalized/ directory
	TargetBackendID   string        `json:"target_backend_id"`
	TargetObjectKey   string        `json:"target_object_key"`
	ExpectedSize      int64         `json:"expected_size"`
	OptionalSHA256    string        `json:"optional_sha256,omitempty"`
	AttemptCount      int           `json:"attempt_count"`
	NextAttemptAt     time.Time     `json:"next_attempt_at"`
	LastError         string        `json:"last_error,omitempty"`
	State             TransferState `json:"state"`
	LockedBy          string        `json:"locked_by,omitempty"`
	LeaseToken        string        `json:"lease_token,omitempty"`
	LeaseExpiresAt    time.Time     `json:"lease_expires_at,omitempty"`
	CreatedAt         time.Time     `json:"created_at"`
	UpdatedAt         time.Time     `json:"updated_at"`
	MaxAttempts       int           `json:"max_attempts"`
}

// NewTransferTask initializes a TransferTask in PENDING state.
func NewTransferTask(id, jobID, assetID, workspaceID, sourceFilename, backendID, targetObjKey string, expectedSize int64) (*TransferTask, error) {
	if id == "" || jobID == "" || assetID == "" || workspaceID == "" || sourceFilename == "" || backendID == "" || targetObjKey == "" {
		return nil, fmt.Errorf("transfer task missing required fields")
	}
	now := time.Now()
	return &TransferTask{
		ID:                id,
		JobID:             jobID,
		AssetID:           assetID,
		SourceWorkspaceID: workspaceID,
		SourceFilename:    sourceFilename,
		TargetBackendID:   backendID,
		TargetObjectKey:   targetObjKey,
		ExpectedSize:      expectedSize,
		State:             TransferPending,
		NextAttemptAt:     now,
		CreatedAt:         now,
		UpdatedAt:         now,
		MaxAttempts:       5,
	}, nil
}

// TransferTaskRepository defines persistent CRUD and CAS claiming operations for TransferTasks.
type TransferTaskRepository interface {
	CreateTask(ctx context.Context, task *TransferTask) error
	SaveTaskLeased(ctx context.Context, task *TransferTask, workerID string, leaseToken string) error
	RenewTaskLease(ctx context.Context, taskID string, workerID string, leaseToken string, extension time.Duration) (time.Time, error)
	RecoverTaskState(ctx context.Context, taskID string, expectedStates []TransferState, requireLeaseExpired bool, newState TransferState, lastError string) error
	Get(ctx context.Context, id string) (*TransferTask, error)
	List(ctx context.Context) ([]*TransferTask, error)
	ClaimTask(ctx context.Context, workerID string, leaseDuration time.Duration) (*TransferTask, error)
	Delete(ctx context.Context, id string) error
}

// DiskTransferTaskRepository implements TransferTaskRepository storing tasks in transfers.json.
type DiskTransferTaskRepository struct {
	mu          sync.RWMutex
	storagePath string
}

// NewDiskTransferTaskRepository initializes DiskTransferTaskRepository storing tasks at storagePath.
func NewDiskTransferTaskRepository(storagePath string) (*DiskTransferTaskRepository, error) {
	if storagePath == "" {
		return nil, fmt.Errorf("storagePath cannot be empty")
	}
	dir := filepath.Dir(storagePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory for transfer task repository: %w", err)
	}
	return &DiskTransferTaskRepository{
		storagePath: storagePath,
	}, nil
}

// CreateTask creates a new TransferTask, failing if task ID already exists.
func (r *DiskTransferTaskRepository) CreateTask(ctx context.Context, task *TransferTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if task == nil || task.ID == "" {
		return fmt.Errorf("task or task ID cannot be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	tasks, err := r.loadLocked()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if tasks == nil {
		tasks = make(map[string]*TransferTask)
	}

	if _, ok := tasks[task.ID]; ok {
		return ErrTransferTaskAlreadyExists
	}

	cp := *task
	cp.UpdatedAt = time.Now()
	tasks[cp.ID] = &cp

	return r.saveLocked(tasks)
}

// SaveTaskLeased updates a TransferTask verifying matching workerID and leaseToken, preserving lease fields directly from disk.
func (r *DiskTransferTaskRepository) SaveTaskLeased(ctx context.Context, task *TransferTask, workerID string, leaseToken string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if task == nil || task.ID == "" || workerID == "" || leaseToken == "" {
		return fmt.Errorf("invalid SaveTaskLeased parameters")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	tasks, err := r.loadLocked()
	if err != nil {
		return err
	}

	existing, ok := tasks[task.ID]
	if !ok {
		return ErrTransferTaskNotFound
	}

	now := time.Now()
	// Strict CAS lease ownership & expiration check
	if existing.LockedBy != workerID || existing.LeaseToken != leaseToken {
		return fmt.Errorf("%w: active lock belongs to worker '%s' (token: '%s'), expected worker '%s' (token: '%s')", ErrWorkerLeaseLost, existing.LockedBy, existing.LeaseToken, workerID, leaseToken)
	}
	if now.After(existing.LeaseExpiresAt) {
		return fmt.Errorf("%w: worker lease expired at %v", ErrWorkerLeaseLost, existing.LeaseExpiresAt)
	}

	cp := *task
	// Authoritatively preserve active lease fields directly from existing record (eliminates in-memory data races)
	cp.LockedBy = existing.LockedBy
	cp.LeaseToken = existing.LeaseToken
	cp.LeaseExpiresAt = existing.LeaseExpiresAt
	cp.UpdatedAt = now
	tasks[cp.ID] = &cp

	return r.saveLocked(tasks)
}

// RenewTaskLease extends active worker lease duration returning new LeaseExpiresAt time. Strictly requires TransferRunning state.
func (r *DiskTransferTaskRepository) RenewTaskLease(ctx context.Context, taskID string, workerID string, leaseToken string, extension time.Duration) (time.Time, error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}
	if taskID == "" || workerID == "" || leaseToken == "" || extension <= 0 {
		return time.Time{}, fmt.Errorf("invalid RenewTaskLease parameters")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	tasks, err := r.loadLocked()
	if err != nil {
		return time.Time{}, err
	}

	existing, ok := tasks[taskID]
	if !ok {
		return time.Time{}, ErrTransferTaskNotFound
	}

	now := time.Now()

	// RenewTaskLease is strictly forbidden on non-RUNNING or already COMPLETED tasks!
	if existing.State != TransferRunning {
		return time.Time{}, fmt.Errorf("%w: task state is %s, must be RUNNING to renew lease", ErrWorkerLeaseLost, existing.State)
	}

	if existing.LockedBy != workerID || existing.LeaseToken != leaseToken || now.After(existing.LeaseExpiresAt) {
		return time.Time{}, fmt.Errorf("%w: cannot renew lease for worker '%s' (token: '%s')", ErrWorkerLeaseLost, workerID, leaseToken)
	}

	newExpiry := now.Add(extension)
	existing.LeaseExpiresAt = newExpiry
	existing.UpdatedAt = now

	if err := r.saveLocked(tasks); err != nil {
		return time.Time{}, err
	}

	return newExpiry, nil
}

// RecoverTaskState updates task state during startup reconciliation enforcing lease expiration & expected states.
func (r *DiskTransferTaskRepository) RecoverTaskState(
	ctx context.Context,
	taskID string,
	expectedStates []TransferState,
	requireLeaseExpired bool,
	newState TransferState,
	lastError string,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if taskID == "" {
		return fmt.Errorf("taskID cannot be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	tasks, err := r.loadLocked()
	if err != nil {
		return err
	}

	existing, ok := tasks[taskID]
	if !ok {
		return ErrTransferTaskNotFound
	}

	now := time.Now()

	// Verify expected state match if specified
	if len(expectedStates) > 0 {
		matched := false
		for _, st := range expectedStates {
			if existing.State == st {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("task state %s does not match required expected states %v", existing.State, expectedStates)
		}
	}

	// Verify lease is expired if requireLeaseExpired is true
	if requireLeaseExpired && existing.State == TransferRunning && now.Before(existing.LeaseExpiresAt) {
		return fmt.Errorf("%w: cannot recover task %s while active worker lease is running until %v", ErrTaskLeaseActive, taskID, existing.LeaseExpiresAt)
	}

	existing.State = newState
	if lastError != "" {
		existing.LastError = lastError
	}
	existing.LockedBy = ""
	existing.LeaseToken = ""
	existing.UpdatedAt = now

	tasks[existing.ID] = existing

	return r.saveLocked(tasks)
}

func (r *DiskTransferTaskRepository) Get(ctx context.Context, id string) (*TransferTask, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	tasks, err := r.loadLocked()
	if err != nil {
		return nil, err
	}

	task, ok := tasks[id]
	if !ok {
		return nil, ErrTransferTaskNotFound
	}
	cp := *task
	return &cp, nil
}

func (r *DiskTransferTaskRepository) List(ctx context.Context) ([]*TransferTask, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	tasksMap, err := r.loadLocked()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var list []*TransferTask
	for _, t := range tasksMap {
		cp := *t
		list = append(list, &cp)
	}
	return list, nil
}

// ClaimTask claims the next runnable TransferTask atomically generating a unique crypto LeaseToken.
func (r *DiskTransferTaskRepository) ClaimTask(ctx context.Context, workerID string, leaseDuration time.Duration) (*TransferTask, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if workerID == "" {
		return nil, fmt.Errorf("workerID cannot be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	tasks, err := r.loadLocked()
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if tasks == nil {
		return nil, ErrNoTransferTaskToClaim
	}

	now := time.Now()
	var candidate *TransferTask

	for _, task := range tasks {
		if task.State == TransferCompleted || task.State == TransferFailed {
			continue
		}
		// Runnable if PENDING or RETRYING and due
		isDue := (task.State == TransferPending || task.State == TransferRetrying) && now.After(task.NextAttemptAt)
		// Or lease expired while RUNNING
		leaseExpired := task.State == TransferRunning && now.After(task.LeaseExpiresAt)

		if isDue || leaseExpired {
			candidate = task
			break
		}
	}

	if candidate == nil {
		return nil, ErrNoTransferTaskToClaim
	}

	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("failed to generate random lease token: %w", err)
	}

	candidate.State = TransferRunning
	candidate.LockedBy = workerID
	candidate.LeaseToken = hex.EncodeToString(tokenBytes)
	candidate.LeaseExpiresAt = now.Add(leaseDuration)
	candidate.AttemptCount++
	candidate.UpdatedAt = now

	cp := *candidate
	tasks[cp.ID] = candidate

	if err := r.saveLocked(tasks); err != nil {
		return nil, err
	}

	return &cp, nil
}

func (r *DiskTransferTaskRepository) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	tasks, err := r.loadLocked()
	if err != nil {
		return err
	}

	if _, ok := tasks[id]; !ok {
		return ErrTransferTaskNotFound
	}

	delete(tasks, id)
	return r.saveLocked(tasks)
}

func (r *DiskTransferTaskRepository) loadLocked() (map[string]*TransferTask, error) {
	data, err := os.ReadFile(r.storagePath)
	if err != nil {
		return nil, err
	}

	var tasks map[string]*TransferTask
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, fmt.Errorf("failed to unmarshal transfer tasks: %w", err)
	}
	return tasks, nil
}

func (r *DiskTransferTaskRepository) saveLocked(tasks map[string]*TransferTask) error {
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := r.storagePath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to open tmp transfer file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write tmp transfer file: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to fsync tmp transfer file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close tmp transfer file cleanly: %w", err)
	}

	if err := os.Rename(tmpPath, r.storagePath); err != nil {
		return fmt.Errorf("failed to rename transfer repository file: %w", err)
	}

	dirPath := filepath.Dir(r.storagePath)
	pDir, err := os.Open(dirPath)
	if err != nil {
		return fmt.Errorf("failed to open transfer repository directory for fsync: %w", err)
	}
	if err := pDir.Sync(); err != nil {
		_ = pDir.Close()
		return fmt.Errorf("failed to fsync transfer repository parent directory: %w", err)
	}
	_ = pDir.Close()

	return nil
}
