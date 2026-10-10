// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package admission

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Controller implements the SlotBudget interface with atomic per-account slot allocation,
// shared stream attachment, channel-switch coordination, and warm-hold eviction.
type Controller struct {
	mu          sync.Mutex
	teardownWG  sync.WaitGroup
	accounts    map[string]*accountState
	isClosed    bool
	closeErr    error
	closeDone   chan struct{}
	nextLeaseID uint64
}

type slotState struct {
	sourceID        string
	allocatedAt     time.Time
	clients         map[string]map[uint64]struct{}
	isHolding       bool
	holdTimer       *time.Timer
	isFenced        bool
	teardownRunning bool
	teardownErr     error
	teardownFn      func() error
}

type accountState struct {
	accountID        string
	maxSlots         int
	warmHoldDuration time.Duration
	slots            map[string]*slotState // key is sourceID
}

// NewController creates an admission controller with optional initial account configurations.
func NewController(configs ...AccountConfig) *Controller {
	c := &Controller{
		accounts:  make(map[string]*accountState),
		closeDone: make(chan struct{}),
	}
	for _, cfg := range configs {
		_ = c.ConfigureAccount(cfg)
	}
	return c
}

// ConfigureAccount adds or updates an account configuration.
func (c *Controller) ConfigureAccount(cfg AccountConfig) error {
	accountID := strings.TrimSpace(cfg.AccountID)
	if accountID == "" {
		return ErrInvalidRequest
	}
	if cfg.MaxSlots <= 0 {
		cfg.MaxSlots = 2 // Baseline default
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isClosed {
		return ErrControllerClosed
	}

	acc, exists := c.accounts[accountID]
	if !exists {
		c.accounts[accountID] = &accountState{
			accountID:        accountID,
			maxSlots:         cfg.MaxSlots,
			warmHoldDuration: cfg.WarmHoldDuration,
			slots:            make(map[string]*slotState),
		}
		return nil
	}

	acc.maxSlots = cfg.MaxSlots
	acc.warmHoldDuration = cfg.WarmHoldDuration
	return nil
}

type leaseImpl struct {
	controller *Controller
	accountID  string
	sourceID   string
	clientID   string
	isShared   bool
	leaseID    uint64
	released   atomic.Bool
	once       sync.Once
	releaseErr error
}

func (l *leaseImpl) AccountID() string { return l.accountID }
func (l *leaseImpl) SourceID() string  { return l.sourceID }
func (l *leaseImpl) ClientID() string  { return l.clientID }
func (l *leaseImpl) LeaseID() uint64   { return l.leaseID }
func (l *leaseImpl) IsShared() bool    { return l.isShared }
func (l *leaseImpl) IsActive() bool {
	return l != nil && l.controller != nil && !l.released.Load() && l.controller.leaseActive(l)
}

func (l *leaseImpl) Release() error {
	l.once.Do(func() {
		l.released.Store(true)
		l.releaseErr = l.controller.releaseLease(l.accountID, l.sourceID, l.clientID, l.leaseID)
	})
	return l.releaseErr
}

func (c *Controller) newLeaseLocked(accountID string, slot *slotState, clientID string, shared bool) SlotLease {
	c.nextLeaseID++
	leaseID := c.nextLeaseID
	if slot.clients[clientID] == nil {
		slot.clients[clientID] = make(map[uint64]struct{})
	}
	slot.clients[clientID][leaseID] = struct{}{}
	return &leaseImpl{controller: c, accountID: accountID, sourceID: slot.sourceID, clientID: clientID, isShared: shared, leaseID: leaseID}
}

func (c *Controller) leaseActive(lease *leaseImpl) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.isClosed {
		return false
	}
	acc := c.accounts[lease.accountID]
	if acc == nil {
		return false
	}
	slot := acc.slots[lease.sourceID]
	if slot == nil || slot.isFenced {
		return false
	}
	_, ok := slot.clients[lease.clientID][lease.leaseID]
	return ok
}

// Acquire requests an upstream stream slot.
func (c *Controller) Acquire(ctx context.Context, req AcquireRequest) (SlotLease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	accountID := strings.TrimSpace(req.AccountID)
	sourceID := strings.TrimSpace(req.SourceID)
	clientID := strings.TrimSpace(req.ClientID)

	if accountID == "" || sourceID == "" || clientID == "" {
		return nil, ErrInvalidRequest
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isClosed {
		return nil, ErrControllerClosed
	}

	acc, exists := c.accounts[accountID]
	if !exists {
		return nil, ErrAccountNotFound
	}

	// 1. Check if sourceID is already streaming and not fenced
	if slot, ok := acc.slots[sourceID]; ok {
		if slot.isFenced {
			return nil, ErrSlotFenced
		}
		if slot.isHolding {
			if slot.holdTimer != nil {
				slot.holdTimer.Stop()
				slot.holdTimer = nil
			}
			slot.isHolding = false
		}
		shared := len(slot.clients) > 0 && slot.clients[clientID] == nil
		return c.newLeaseLocked(accountID, slot, clientID, shared), nil
	}

	// 2. Count active non-fenced slots
	activeCount := acc.activeSlotCountLocked()
	if activeCount >= acc.maxSlots {
		// Attempt warm-hold eviction
		if evicted, err := c.evictHoldingSlotLocked(acc); err != nil {
			return nil, err
		} else if evicted {
			activeCount--
		}
	}

	if activeCount >= acc.maxSlots {
		return nil, ErrCapacityExceeded
	}

	// 3. Allocate new slot
	slot := &slotState{
		sourceID:    sourceID,
		allocatedAt: time.Now(),
		clients:     make(map[string]map[uint64]struct{}),
	}
	acc.slots[sourceID] = slot

	return c.newLeaseLocked(accountID, slot, clientID, false), nil
}

// Switch coordinates an atomic channel switch for a client.
func (c *Controller) Switch(ctx context.Context, req SwitchRequest) (SlotLease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	accountID := strings.TrimSpace(req.AccountID)
	oldSourceID := strings.TrimSpace(req.OldSourceID)
	newSourceID := strings.TrimSpace(req.NewSourceID)
	clientID := strings.TrimSpace(req.ClientID)
	if accountID == "" || oldSourceID == "" || newSourceID == "" || clientID == "" {
		return nil, ErrInvalidRequest
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.isClosed {
		return nil, ErrControllerClosed
	}
	acc, exists := c.accounts[accountID]
	if !exists {
		return nil, ErrAccountNotFound
	}

	oldSlot := acc.slots[oldSourceID]
	if oldSlot == nil || oldSlot.isFenced {
		return nil, ErrInvalidRequest
	}
	leases := oldSlot.clients[clientID]
	if len(leases) == 0 {
		return nil, ErrInvalidRequest
	}
	leaseID := req.LeaseID
	if leaseID == 0 {
		if len(leases) != 1 {
			return nil, ErrInvalidRequest
		}
		for id := range leases {
			leaseID = id
		}
	}
	if _, exists := leases[leaseID]; !exists {
		return nil, ErrInvalidRequest
	}
	if oldSourceID == newSourceID {
		shared := len(oldSlot.clients) > 1
		return c.newLeaseLocked(accountID, oldSlot, clientID, shared), nil
	}
	if target := acc.slots[newSourceID]; target != nil && target.isFenced {
		return nil, ErrSlotFenced
	}

	// A source stays active while any viewer or additional lease remains.
	// Reserve destination capacity before detaching the selected lease so a
	// rejected zap is lossless.
	sharedOldSlot := len(oldSlot.clients) > 1 || len(leases) > 1
	if sharedOldSlot {
		target := acc.slots[newSourceID]
		if target == nil {
			activeCount := acc.activeSlotCountLocked()
			if activeCount >= acc.maxSlots {
				evicted, err := c.evictHoldingSlotLocked(acc)
				if err != nil {
					return nil, err
				}
				if evicted {
					activeCount--
				}
			}
			if activeCount >= acc.maxSlots {
				return nil, ErrCapacityExceeded
			}
		}
		// Teardown may have temporarily released the mutex while evicting a
		// warm slot. Revalidate the source and viewer before committing the zap.
		if acc.slots[oldSourceID] != oldSlot || oldSlot.isFenced {
			return nil, ErrSlotFenced
		}
		if _, stillAttached := oldSlot.clients[clientID][leaseID]; !stillAttached {
			return nil, ErrInvalidRequest
		}
		target = acc.slots[newSourceID]
		if target != nil && target.isFenced {
			return nil, ErrSlotFenced
		}
		if target == nil && acc.activeSlotCountLocked() >= acc.maxSlots {
			return nil, ErrCapacityExceeded
		}
		delete(oldSlot.clients[clientID], leaseID)
		if len(oldSlot.clients[clientID]) == 0 {
			delete(oldSlot.clients, clientID)
		}
		if target != nil {
			if target.isHolding {
				if target.holdTimer != nil {
					target.holdTimer.Stop()
					target.holdTimer = nil
				}
				target.isHolding = false
			}
			return c.newLeaseLocked(accountID, target, clientID, len(target.clients) > 0), nil
		}
		target = &slotState{sourceID: newSourceID, allocatedAt: time.Now(), clients: make(map[string]map[uint64]struct{})}
		acc.slots[newSourceID] = target
		return c.newLeaseLocked(accountID, target, clientID, false), nil
	}

	// A sole viewer can free its old upstream first. The fenced old slot remains
	// counted until teardown succeeds, so a failed close cannot admit a third
	// provider connection.
	delete(oldSlot.clients[clientID], leaseID)
	delete(oldSlot.clients, clientID)
	if err := c.stopSlotLocked(acc, oldSourceID, oldSlot); err != nil {
		return nil, err
	}

	if target := acc.slots[newSourceID]; target != nil {
		if target.isFenced {
			return nil, ErrSlotFenced
		}
		if target.isHolding {
			if target.holdTimer != nil {
				target.holdTimer.Stop()
				target.holdTimer = nil
			}
			target.isHolding = false
		}
		return c.newLeaseLocked(accountID, target, clientID, len(target.clients) > 0), nil
	}
	if acc.activeSlotCountLocked() >= acc.maxSlots {
		return nil, ErrCapacityExceeded
	}
	target := &slotState{sourceID: newSourceID, allocatedAt: time.Now(), clients: make(map[string]map[uint64]struct{})}
	acc.slots[newSourceID] = target
	return c.newLeaseLocked(accountID, target, clientID, false), nil
}

func (c *Controller) releaseLease(accountID, sourceID, clientID string, leaseID uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isClosed {
		return nil
	}
	acc := c.accounts[accountID]
	if acc == nil {
		return nil
	}
	slot := acc.slots[sourceID]
	if slot == nil || slot.isFenced {
		return nil
	}
	leases := slot.clients[clientID]
	if _, exists := leases[leaseID]; !exists {
		return nil
	}
	delete(leases, leaseID)
	if len(leases) > 0 {
		return nil
	}
	delete(slot.clients, clientID)
	if len(slot.clients) > 0 {
		return nil
	}

	if acc.warmHoldDuration > 0 {
		slot.isHolding = true
		slot.holdTimer = time.AfterFunc(acc.warmHoldDuration, func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.isClosed || c.accounts[accountID] != acc || acc.slots[sourceID] != slot {
				return
			}
			if slot.isHolding && len(slot.clients) == 0 {
				if err := c.stopSlotLocked(acc, sourceID, slot); err != nil {
					slot.teardownErr = err
				}
			}
		})
		return nil
	}
	return c.stopSlotLocked(acc, sourceID, slot)
}

// stopSlotLocked fences a slot before running external teardown without holding
// the controller mutex. The caller must hold c.mu on entry and return.
func (c *Controller) stopSlotLocked(acc *accountState, sourceID string, slot *slotState) error {
	slot.isFenced = true
	slot.isHolding = false
	if slot.holdTimer != nil {
		slot.holdTimer.Stop()
		slot.holdTimer = nil
	}
	if slot.teardownRunning {
		return ErrSlotFenced
	}
	if slot.teardownFn == nil {
		if acc.slots[sourceID] == slot {
			delete(acc.slots, sourceID)
		}
		return nil
	}

	slot.teardownRunning = true
	c.teardownWG.Add(1)
	c.mu.Unlock()
	err := invokeTeardown(slot.teardownFn)
	c.mu.Lock()
	slot.teardownRunning = false
	slot.teardownErr = err
	c.teardownWG.Done()

	if err != nil {
		return errors.Join(ErrTeardownFailed, err)
	}
	if acc.slots[sourceID] == slot {
		delete(acc.slots, sourceID)
	}
	return nil
}

func invokeTeardown(fn func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("teardown panicked: %v", recovered)
		}
	}()
	return fn()
}

// RegisterTeardown attaches an upstream termination hook to an active slot.
func (c *Controller) RegisterTeardown(accountID, sourceID string, fn func() error) error {
	accountID = strings.TrimSpace(accountID)
	sourceID = strings.TrimSpace(sourceID)
	if accountID == "" || sourceID == "" || fn == nil {
		return ErrInvalidRequest
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.isClosed {
		return ErrControllerClosed
	}
	acc := c.accounts[accountID]
	if acc == nil {
		return ErrAccountNotFound
	}
	slot := acc.slots[sourceID]
	if slot == nil || slot.isFenced {
		return ErrSlotFenced
	}
	slot.teardownFn = fn
	return nil
}

func (c *Controller) evictHoldingSlotLocked(acc *accountState) (bool, error) {
	for id, slot := range acc.slots {
		if slot.isHolding && len(slot.clients) == 0 && !slot.isFenced {
			if err := c.stopSlotLocked(acc, id, slot); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	return false, nil
}

func (acc *accountState) activeSlotCountLocked() int {
	// Fenced slots continue consuming capacity until teardown succeeds.
	return len(acc.slots)
}

// Usage returns sanitized capacity metrics for an account.
func (c *Controller) Usage(accountID string) (AccountUsage, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return AccountUsage{}, ErrInvalidRequest
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	acc, exists := c.accounts[accountID]
	if !exists {
		return AccountUsage{}, ErrAccountNotFound
	}

	sources := make([]SourceUsage, 0, len(acc.slots))
	totalViewers := 0
	for _, slot := range acc.slots {
		subCount := len(slot.clients)
		totalViewers += subCount
		sources = append(sources, SourceUsage{
			SourceID:    slot.sourceID,
			Subscribers: subCount,
			IsHolding:   slot.isHolding,
			IsFenced:    slot.isFenced,
			AllocatedAt: slot.allocatedAt,
		})
	}
	return AccountUsage{
		AccountID:     acc.accountID,
		ActiveSlots:   len(sources),
		MaxSlots:      acc.maxSlots,
		TotalViewers:  totalViewers,
		ActiveSources: sources,
	}, nil
}

type teardownTask struct {
	sourceID string
	account  *accountState
	slot     *slotState
	fn       func() error
}

// Close fences all slots and runs upstream teardown hooks. A failed teardown is
// returned to the caller rather than being silently treated as a released slot.
func (c *Controller) Close() error {
	c.mu.Lock()
	if c.isClosed {
		done := c.closeDone
		c.mu.Unlock()
		<-done
		c.mu.Lock()
		err := c.closeErr
		c.mu.Unlock()
		return err
	}
	c.isClosed = true

	var tasks []teardownTask
	for _, acc := range c.accounts {
		for sourceID, slot := range acc.slots {
			slot.isFenced = true
			slot.isHolding = false
			if slot.holdTimer != nil {
				slot.holdTimer.Stop()
				slot.holdTimer = nil
			}
			if slot.teardownFn == nil || slot.teardownRunning {
				continue
			}
			slot.teardownRunning = true
			c.teardownWG.Add(1)
			tasks = append(tasks, teardownTask{sourceID: sourceID, account: acc, slot: slot, fn: slot.teardownFn})
		}
	}
	c.mu.Unlock()

	for _, task := range tasks {
		err := invokeTeardown(task.fn)
		c.mu.Lock()
		task.slot.teardownRunning = false
		task.slot.teardownErr = err
		if err == nil && task.account.slots[task.sourceID] == task.slot {
			delete(task.account.slots, task.sourceID)
		}
		c.mu.Unlock()
		c.teardownWG.Done()
	}
	c.teardownWG.Wait()

	c.mu.Lock()
	var closeErrors []error
	for accountID, acc := range c.accounts {
		for sourceID, slot := range acc.slots {
			if slot.teardownErr != nil {
				closeErrors = append(closeErrors, fmt.Errorf("teardown %s/%s: %w", accountID, sourceID, slot.teardownErr))
			} else if slot.teardownFn == nil {
				delete(acc.slots, sourceID)
			}
		}
	}
	c.closeErr = errors.Join(closeErrors...)
	close(c.closeDone)
	err := c.closeErr
	c.mu.Unlock()
	return err
}
