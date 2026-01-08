package vod

import (
	"os"
	"sync"
)

// MockFS implements FS interface for testing.
type MockFS struct {
	mu             sync.Mutex
	renameCalls    []RenameCall
	removeAllCalls []string
	statCalls      []string
	renameErr      error
	removeAllErr   error
}

// RenameCall records a call to Rename.
type RenameCall struct {
	Old string
	New string
}

func (m *MockFS) Rename(oldpath, newpath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.renameCalls = append(m.renameCalls, RenameCall{Old: oldpath, New: newpath})
	return m.renameErr
}

func (m *MockFS) RemoveAll(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeAllCalls = append(m.removeAllCalls, path)
	return m.removeAllErr
}

func (m *MockFS) Stat(name string) (os.FileInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statCalls = append(m.statCalls, name)
	return nil, os.ErrNotExist // Default: file doesn't exist
}

// GetRenameCalls returns all recorded Rename calls.
func (m *MockFS) GetRenameCalls() []RenameCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]RenameCall{}, m.renameCalls...)
}

// GetRemoveAllCalls returns all recorded RemoveAll calls.
func (m *MockFS) GetRemoveAllCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.removeAllCalls...)
}
