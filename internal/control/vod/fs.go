package vod

import "os"

// FS abstracts filesystem operations for testability.
type FS interface {
	// Rename atomically moves a file from old path to new path.
	Rename(oldpath, newpath string) error

	// RemoveAll removes path and any children it contains.
	RemoveAll(path string) error

	// Stat returns FileInfo for the named file.
	Stat(name string) (os.FileInfo, error)
	// MkdirAll creates a directory and any necessary parents.
	MkdirAll(path string, perm os.FileMode) error
}

// RealFS uses actual os operations.
type RealFS struct{}

func (RealFS) Rename(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}

func (RealFS) RemoveAll(path string) error {
	return os.RemoveAll(path)
}

func (RealFS) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

func (RealFS) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}
