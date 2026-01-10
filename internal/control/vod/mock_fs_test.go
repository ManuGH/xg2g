package vod

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

// MockFS implements FS interface for testing.
type MockFS struct {
	mu             sync.Mutex
	renameCalls    []RenameCall
	removeAllCalls []string
	statCalls      []string
	renameErr      error
	removeAllErr   error
	exists         map[string]bool
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
	if m.exists != nil {
		if exists, ok := m.exists[name]; ok && exists {
			return fakeFileInfo{name: filepath.Base(name)}, nil
		}
	}
	return nil, os.ErrNotExist
}

func (m *MockFS) MkdirAll(path string, perm os.FileMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.exists == nil {
		m.exists = make(map[string]bool)
	}
	m.exists[path] = true
	return nil
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

func (m *MockFS) SetExists(path string, exists bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.exists == nil {
		m.exists = make(map[string]bool)
	}
	m.exists[path] = exists
}

type fakeFileInfo struct {
	name string
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() os.FileMode  { return 0 }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() interface{}   { return nil }
