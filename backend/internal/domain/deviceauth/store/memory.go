// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"context"
	"sync"

	"github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
)

type MemoryStateStore struct {
	mu sync.RWMutex

	pairings       map[string]model.PairingRecord
	pairingsByCode map[string]string
	devices        map[string]model.DeviceRecord
	grants         map[string]model.DeviceGrantRecord
	sessions       map[string]model.AccessSessionRecord
	webBootstraps  map[string]model.WebBootstrapRecord
}

func NewMemoryStateStore() *MemoryStateStore {
	return &MemoryStateStore{
		pairings:       make(map[string]model.PairingRecord),
		pairingsByCode: make(map[string]string),
		devices:        make(map[string]model.DeviceRecord),
		grants:         make(map[string]model.DeviceGrantRecord),
		sessions:       make(map[string]model.AccessSessionRecord),
		webBootstraps:  make(map[string]model.WebBootstrapRecord),
	}
}

func (s *MemoryStateStore) GetPairing(_ context.Context, pairingID string) (*model.PairingRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	record, ok := s.pairings[pairingID]
	if !ok {
		return nil, ErrNotFound
	}
	cloned, err := clonePairing(record)
	if err != nil {
		return nil, err
	}
	return &cloned, nil
}

func (s *MemoryStateStore) GetPairingByUserCode(_ context.Context, userCode string) (*model.PairingRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pairingID, ok := s.pairingsByCode[userCode]
	if !ok {
		return nil, ErrNotFound
	}
	record, ok := s.pairings[pairingID]
	if !ok {
		return nil, ErrNotFound
	}
	cloned, err := clonePairing(record)
	if err != nil {
		return nil, err
	}
	return &cloned, nil
}

func (s *MemoryStateStore) PutPairing(_ context.Context, record *model.PairingRecord) error {
	if record == nil {
		return model.ErrInvalidPairingID
	}
	prepared, err := clonePairing(*record)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existingID, ok := s.pairingsByCode[prepared.UserCode]; ok && existingID != prepared.PairingID {
		return ErrConflict
	}
	if existing, ok := s.pairings[prepared.PairingID]; ok && existing.UserCode != prepared.UserCode {
		delete(s.pairingsByCode, existing.UserCode)
	}

	s.pairings[prepared.PairingID] = prepared
	s.pairingsByCode[prepared.UserCode] = prepared.PairingID
	return nil
}

func (s *MemoryStateStore) UpdatePairing(_ context.Context, pairingID string, fn func(*model.PairingRecord) error) (*model.PairingRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.pairings[pairingID]
	if !ok {
		return nil, ErrNotFound
	}
	working, err := clonePairing(record)
	if err != nil {
		return nil, err
	}
	if err := fn(&working); err != nil {
		return nil, err
	}
	working, err = clonePairing(working)
	if err != nil {
		return nil, err
	}
	if existingID, ok := s.pairingsByCode[working.UserCode]; ok && existingID != working.PairingID {
		return nil, ErrConflict
	}
	if record.UserCode != working.UserCode {
		delete(s.pairingsByCode, record.UserCode)
	}
	s.pairings[working.PairingID] = working
	s.pairingsByCode[working.UserCode] = working.PairingID
	return ptrPairing(working)
}

func (s *MemoryStateStore) PutDevice(_ context.Context, record *model.DeviceRecord) error {
	if record == nil {
		return model.ErrInvalidDeviceID
	}
	prepared, err := cloneDevice(*record)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.devices[prepared.DeviceID] = prepared
	return nil
}

func (s *MemoryStateStore) GetDevice(_ context.Context, deviceID string) (*model.DeviceRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.devices[deviceID]
	if !ok {
		return nil, ErrNotFound
	}
	cloned, err := cloneDevice(record)
	if err != nil {
		return nil, err
	}
	return &cloned, nil
}

func (s *MemoryStateStore) ListDevicesByOwner(_ context.Context, ownerID string) ([]model.DeviceRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.DeviceRecord, 0)
	for _, record := range s.devices {
		if record.OwnerID != ownerID {
			continue
		}
		cloned, err := cloneDevice(record)
		if err != nil {
			return nil, err
		}
		out = append(out, cloned)
	}
	return out, nil
}

func (s *MemoryStateStore) UpdateDevice(_ context.Context, deviceID string, fn func(*model.DeviceRecord) error) (*model.DeviceRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.devices[deviceID]
	if !ok {
		return nil, ErrNotFound
	}
	working, err := cloneDevice(record)
	if err != nil {
		return nil, err
	}
	if err := fn(&working); err != nil {
		return nil, err
	}
	working, err = cloneDevice(working)
	if err != nil {
		return nil, err
	}
	s.devices[working.DeviceID] = working
	return ptrDevice(working)
}

func (s *MemoryStateStore) PutDeviceGrant(_ context.Context, record *model.DeviceGrantRecord) error {
	if record == nil {
		return model.ErrInvalidGrantID
	}
	prepared, err := cloneGrant(*record)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.grants[prepared.GrantID] = prepared
	return nil
}

func (s *MemoryStateStore) GetDeviceGrant(_ context.Context, grantID string) (*model.DeviceGrantRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.grants[grantID]
	if !ok {
		return nil, ErrNotFound
	}
	cloned, err := cloneGrant(record)
	if err != nil {
		return nil, err
	}
	return &cloned, nil
}

func (s *MemoryStateStore) GetActiveDeviceGrantByDevice(_ context.Context, deviceID string) (*model.DeviceGrantRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var selected *model.DeviceGrantRecord
	for _, record := range s.grants {
		if record.DeviceID != deviceID || record.IsRevoked() {
			continue
		}
		cloned, err := cloneGrant(record)
		if err != nil {
			return nil, err
		}
		if selected == nil || cloned.IssuedAt.After(selected.IssuedAt) {
			selected = &cloned
		}
	}
	if selected == nil {
		return nil, ErrNotFound
	}
	return selected, nil
}

func (s *MemoryStateStore) ListDeviceGrantsByDevice(_ context.Context, deviceID string) ([]model.DeviceGrantRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.DeviceGrantRecord, 0)
	for _, record := range s.grants {
		if record.DeviceID != deviceID {
			continue
		}
		cloned, err := cloneGrant(record)
		if err != nil {
			return nil, err
		}
		out = append(out, cloned)
	}
	return out, nil
}

func (s *MemoryStateStore) UpdateDeviceGrant(_ context.Context, grantID string, fn func(*model.DeviceGrantRecord) error) (*model.DeviceGrantRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.grants[grantID]
	if !ok {
		return nil, ErrNotFound
	}
	working, err := cloneGrant(record)
	if err != nil {
		return nil, err
	}
	if err := fn(&working); err != nil {
		return nil, err
	}
	working, err = cloneGrant(working)
	if err != nil {
		return nil, err
	}
	s.grants[working.GrantID] = working
	return ptrGrant(working)
}

func (s *MemoryStateStore) PutAccessSession(_ context.Context, record *model.AccessSessionRecord) error {
	if record == nil {
		return model.ErrInvalidSessionID
	}
	prepared, err := cloneSession(*record)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[prepared.SessionID] = prepared
	return nil
}

func (s *MemoryStateStore) GetAccessSession(_ context.Context, sessionID string) (*model.AccessSessionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.sessions[sessionID]
	if !ok {
		return nil, ErrNotFound
	}
	cloned, err := cloneSession(record)
	if err != nil {
		return nil, err
	}
	return &cloned, nil
}

func (s *MemoryStateStore) GetAccessSessionByTokenHash(_ context.Context, tokenHash string) (*model.AccessSessionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, record := range s.sessions {
		if record.TokenHash != tokenHash {
			continue
		}
		cloned, err := cloneSession(record)
		if err != nil {
			return nil, err
		}
		return &cloned, nil
	}
	return nil, ErrNotFound
}

func (s *MemoryStateStore) ListAccessSessionsByDevice(_ context.Context, deviceID string) ([]model.AccessSessionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.AccessSessionRecord, 0)
	for _, record := range s.sessions {
		if record.DeviceID != deviceID {
			continue
		}
		cloned, err := cloneSession(record)
		if err != nil {
			return nil, err
		}
		out = append(out, cloned)
	}
	return out, nil
}

func (s *MemoryStateStore) UpdateAccessSession(_ context.Context, sessionID string, fn func(*model.AccessSessionRecord) error) (*model.AccessSessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.sessions[sessionID]
	if !ok {
		return nil, ErrNotFound
	}
	working, err := cloneSession(record)
	if err != nil {
		return nil, err
	}
	if err := fn(&working); err != nil {
		return nil, err
	}
	working, err = cloneSession(working)
	if err != nil {
		return nil, err
	}
	s.sessions[working.SessionID] = working
	return ptrSession(working)
}

func (s *MemoryStateStore) DeleteAccessSession(_ context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[sessionID]; !ok {
		return ErrNotFound
	}
	delete(s.sessions, sessionID)
	return nil
}

func (s *MemoryStateStore) DeleteAccessSessionsByDevice(_ context.Context, deviceID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	deleted := 0
	for sessionID, record := range s.sessions {
		if record.DeviceID != deviceID {
			continue
		}
		delete(s.sessions, sessionID)
		deleted++
	}
	return deleted, nil
}

func (s *MemoryStateStore) PutWebBootstrap(_ context.Context, record *model.WebBootstrapRecord) error {
	if record == nil {
		return model.ErrInvalidWebBootstrapID
	}
	prepared, err := cloneWebBootstrap(*record)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.webBootstraps[prepared.BootstrapID] = prepared
	return nil
}

func (s *MemoryStateStore) GetWebBootstrap(_ context.Context, bootstrapID string) (*model.WebBootstrapRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.webBootstraps[bootstrapID]
	if !ok {
		return nil, ErrNotFound
	}
	cloned, err := cloneWebBootstrap(record)
	if err != nil {
		return nil, err
	}
	return &cloned, nil
}

func (s *MemoryStateStore) UpdateWebBootstrap(_ context.Context, bootstrapID string, fn func(*model.WebBootstrapRecord) error) (*model.WebBootstrapRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.webBootstraps[bootstrapID]
	if !ok {
		return nil, ErrNotFound
	}
	working, err := cloneWebBootstrap(record)
	if err != nil {
		return nil, err
	}
	if err := fn(&working); err != nil {
		return nil, err
	}
	working, err = cloneWebBootstrap(working)
	if err != nil {
		return nil, err
	}
	s.webBootstraps[working.BootstrapID] = working
	return ptrWebBootstrap(working)
}

func clonePairing(record model.PairingRecord) (model.PairingRecord, error) {
	return model.PreparePairingRecord(record)
}

func cloneDevice(record model.DeviceRecord) (model.DeviceRecord, error) {
	return model.PrepareDeviceRecord(record)
}

func cloneGrant(record model.DeviceGrantRecord) (model.DeviceGrantRecord, error) {
	return model.PrepareDeviceGrantRecord(record)
}

func cloneSession(record model.AccessSessionRecord) (model.AccessSessionRecord, error) {
	return model.PrepareAccessSessionRecord(record)
}

func cloneWebBootstrap(record model.WebBootstrapRecord) (model.WebBootstrapRecord, error) {
	return model.PrepareWebBootstrapRecord(record)
}

func ptrPairing(record model.PairingRecord) (*model.PairingRecord, error) {
	cloned, err := clonePairing(record)
	if err != nil {
		return nil, err
	}
	return &cloned, nil
}

func ptrDevice(record model.DeviceRecord) (*model.DeviceRecord, error) {
	cloned, err := cloneDevice(record)
	if err != nil {
		return nil, err
	}
	return &cloned, nil
}

func ptrGrant(record model.DeviceGrantRecord) (*model.DeviceGrantRecord, error) {
	cloned, err := cloneGrant(record)
	if err != nil {
		return nil, err
	}
	return &cloned, nil
}

func ptrSession(record model.AccessSessionRecord) (*model.AccessSessionRecord, error) {
	cloned, err := cloneSession(record)
	if err != nil {
		return nil, err
	}
	return &cloned, nil
}

func ptrWebBootstrap(record model.WebBootstrapRecord) (*model.WebBootstrapRecord, error) {
	cloned, err := cloneWebBootstrap(record)
	if err != nil {
		return nil, err
	}
	return &cloned, nil
}
