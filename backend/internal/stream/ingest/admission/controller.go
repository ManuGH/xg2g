// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package admission

import (
	"context"
	"strings"
	"sync"
	"time"
)

// Controller implements the SlotBudget interface with atomic per-account slot allocation,
// shared stream attachment, channel-switch coordination, and warm-hold eviction.
type Controller struct {
	mu       sync.Mutex
	accounts map[string]*accountState
	isClosed bool
}

type slotState struct {
	sourceID    string
	allocatedAt time.Time
	clients     map[string]struct{}
	isHolding   bool
	holdTimer   *time.Timer
	isFenced    bool
	teardownFn  func() error
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
		accounts: make(map[string]*accountState),
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

	once sync.Once
}

func (l *leaseImpl) AccountID() string { return l.accountID }
func (l *leaseImpl) SourceID() string  { return l.sourceID }
func (l *leaseImpl) ClientID() string  { return l.clientID }
func (l *leaseImpl) IsShared() bool    { return l.isShared }

func (l *leaseImpl) Release() error {
	var err error
	l.once.Do(func() {
		err = l.controller.releaseLease(l.accountID, l.sourceID, l.clientID)
	})
	return err
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
	if slot, ok := acc.slots[sourceID]; ok && !slot.isFenced {
		if slot.isHolding {
			if slot.holdTimer != nil {
				slot.holdTimer.Stop()
				slot.holdTimer = nil
			}
			slot.isHolding = false
		}
		slot.clients[clientID] = struct{}{}
		return &leaseImpl{
			controller: c,
			accountID:  accountID,
			sourceID:   sourceID,
			clientID:   clientID,
			isShared:   len(slot.clients) > 1,
		}, nil
	}

	// 2. Count active non-fenced slots
	activeCount := acc.activeSlotCountLocked()
	if activeCount >= acc.maxSlots {
		// Attempt warm-hold eviction
		if evicted := acc.evictHoldingSlotLocked(); evicted {
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
		clients:     map[string]struct{}{clientID: {}},
	}
	acc.slots[sourceID] = slot

	return &leaseImpl{
		controller: c,
		accountID:  accountID,
		sourceID:   sourceID,
		clientID:   clientID,
		isShared:   false,
	}, nil
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

	if oldSourceID == newSourceID {
		if slot, ok := acc.slots[oldSourceID]; ok && !slot.isFenced {
			if _, hasClient := slot.clients[clientID]; hasClient {
				return &leaseImpl{
					controller: c,
					accountID:  accountID,
					sourceID:   oldSourceID,
					clientID:   clientID,
					isShared:   len(slot.clients) > 1,
				}, nil
			}
		}
	}

	oldSlot, hasOld := acc.slots[oldSourceID]
	if !hasOld || oldSlot.isFenced {
		return nil, ErrInvalidRequest
	}
	if _, clientOnOld := oldSlot.clients[clientID]; !clientOnOld {
		return nil, ErrInvalidRequest
	}

	isSoleSubscriber := len(oldSlot.clients) == 1

	if isSoleSubscriber {
		// Client is sole subscriber on old slot: we can directly close oldSlot to release the slot
		oldSlot.isFenced = true
		delete(oldSlot.clients, clientID)
		if oldSlot.holdTimer != nil {
			oldSlot.holdTimer.Stop()
			oldSlot.holdTimer = nil
		}
		if oldSlot.teardownFn != nil {
			_ = oldSlot.teardownFn()
		}
		delete(acc.slots, oldSourceID)

		// Now acquire new source
		if newSlot, ok := acc.slots[newSourceID]; ok && !newSlot.isFenced {
			if newSlot.isHolding {
				if newSlot.holdTimer != nil {
					newSlot.holdTimer.Stop()
					newSlot.holdTimer = nil
				}
				newSlot.isHolding = false
			}
			newSlot.clients[clientID] = struct{}{}
			return &leaseImpl{
				controller: c,
				accountID:  accountID,
				sourceID:   newSourceID,
				clientID:   clientID,
				isShared:   len(newSlot.clients) > 1,
			}, nil
		}

		// Allocate new slot
		newSlot := &slotState{
			sourceID:    newSourceID,
			allocatedAt: time.Now(),
			clients:     map[string]struct{}{clientID: {}},
		}
		acc.slots[newSourceID] = newSlot
		return &leaseImpl{
			controller: c,
			accountID:  accountID,
			sourceID:   newSourceID,
			clientID:   clientID,
			isShared:   false,
		}, nil
	}

	// Client is NOT sole subscriber: oldSlot must continue running uninterrupted for others
	// Check if newSourceID is already active
	if newSlot, ok := acc.slots[newSourceID]; ok && !newSlot.isFenced {
		delete(oldSlot.clients, clientID)
		if newSlot.isHolding {
			if newSlot.holdTimer != nil {
				newSlot.holdTimer.Stop()
				newSlot.holdTimer = nil
			}
			newSlot.isHolding = false
		}
		newSlot.clients[clientID] = struct{}{}
		return &leaseImpl{
			controller: c,
			accountID:  accountID,
			sourceID:   newSourceID,
			clientID:   clientID,
			isShared:   len(newSlot.clients) > 1,
		}, nil
	}

	// Need a new slot; check capacity
	activeCount := acc.activeSlotCountLocked()
	if activeCount >= acc.maxSlots {
		if evicted := acc.evictHoldingSlotLocked(); evicted {
			activeCount--
		}
	}

	if activeCount >= acc.maxSlots {
		// Rollback: client stays on oldSlot
		return nil, ErrCapacityExceeded
	}

	delete(oldSlot.clients, clientID)
	newSlot := &slotState{
		sourceID:    newSourceID,
		allocatedAt: time.Now(),
		clients:     map[string]struct{}{clientID: {}},
	}
	acc.slots[newSourceID] = newSlot
	return &leaseImpl{
		controller: c,
		accountID:  accountID,
		sourceID:   newSourceID,
		clientID:   clientID,
		isShared:   false,
	}, nil
}

func (c *Controller) releaseLease(accountID, sourceID, clientID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isClosed {
		return nil
	}

	acc, exists := c.accounts[accountID]
	if !exists {
		return nil
	}

	slot, ok := acc.slots[sourceID]
	if !ok || slot.isFenced {
		return nil
	}

	delete(slot.clients, clientID)
	if len(slot.clients) > 0 {
		return nil
	}

	// 0 active subscribers remaining
	if acc.warmHoldDuration > 0 {
		slot.isHolding = true
		slot.holdTimer = time.AfterFunc(acc.warmHoldDuration, func() {
			c.mu.Lock()
			defer c.mu.Unlock()

			if c.isClosed {
				return
			}
			curAcc, ok := c.accounts[accountID]
			if !ok {
				return
			}
			curSlot, ok := curAcc.slots[sourceID]
			if !ok || curSlot != slot {
				return
			}
			if curSlot.isHolding && len(curSlot.clients) == 0 {
				curSlot.isFenced = true
				if curSlot.teardownFn != nil {
					_ = curSlot.teardownFn()
				}
				delete(curAcc.slots, sourceID)
			}
		})
		return nil
	}

	slot.isFenced = true
	if slot.teardownFn != nil {
		_ = slot.teardownFn()
	}
	delete(acc.slots, sourceID)
	return nil
}

// RegisterTeardown attaches an upstream termination hook to an active slot.
func (c *Controller) RegisterTeardown(accountID, sourceID string, fn func() error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if acc, ok := c.accounts[accountID]; ok {
		if slot, ok := acc.slots[sourceID]; ok && !slot.isFenced {
			slot.teardownFn = fn
		}
	}
}

func (acc *accountState) activeSlotCountLocked() int {
	count := 0
	for _, s := range acc.slots {
		if !s.isFenced {
			count++
		}
	}
	return count
}

func (acc *accountState) evictHoldingSlotLocked() bool {
	for id, s := range acc.slots {
		if s.isHolding && len(s.clients) == 0 && !s.isFenced {
			s.isFenced = true
			if s.holdTimer != nil {
				s.holdTimer.Stop()
				s.holdTimer = nil
			}
			if s.teardownFn != nil {
				_ = s.teardownFn()
			}
			delete(acc.slots, id)
			return true
		}
	}
	return false
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

	var sources []SourceUsage
	totalViewers := 0

	for _, s := range acc.slots {
		if s.isFenced {
			continue
		}
		subCount := len(s.clients)
		totalViewers += subCount
		sources = append(sources, SourceUsage{
			SourceID:    s.sourceID,
			Subscribers: subCount,
			IsHolding:   s.isHolding,
			AllocatedAt: s.allocatedAt,
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

// Close gracefully releases all slots and stops background timers.
func (c *Controller) Close() error {
	c.mu.Lock()
	if c.isClosed {
		c.mu.Unlock()
		return nil
	}
	c.isClosed = true

	var teardowns []func() error
	for _, acc := range c.accounts {
		for _, s := range acc.slots {
			if s.holdTimer != nil {
				s.holdTimer.Stop()
				s.holdTimer = nil
			}
			s.isFenced = true
			if s.teardownFn != nil {
				teardowns = append(teardowns, s.teardownFn)
			}
		}
		acc.slots = make(map[string]*slotState)
	}
	c.mu.Unlock()

	for _, fn := range teardowns {
		_ = fn()
	}
	return nil
}
