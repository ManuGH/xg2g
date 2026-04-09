// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import "os"

var (
	processLookupEnv envLookupFunc                       = os.LookupEnv
	processGetEnv    func(string) string                 = os.Getenv
	processEnviron   func() []string                     = os.Environ
	processReadDir   func(string) ([]os.DirEntry, error) = os.ReadDir
)

func currentProcessLookupEnv() envLookupFunc {
	if processLookupEnv == nil {
		return func(string) (string, bool) { return "", false }
	}
	return processLookupEnv
}

// HasProcessEnv reports whether the given environment key is explicitly present
// in the current process environment. This keeps direct environment inspection
// inside internal/config.
func HasProcessEnv(key string) bool {
	_, ok := currentProcessLookupEnv()(key)
	return ok
}

func currentProcessGetEnv() func(string) string {
	if processGetEnv == nil {
		return func(string) string { return "" }
	}
	return processGetEnv
}

func currentProcessEnviron() func() []string {
	if processEnviron == nil {
		return func() []string { return nil }
	}
	return processEnviron
}

func currentProcessReadDir() func(string) ([]os.DirEntry, error) {
	if processReadDir == nil {
		return func(string) ([]os.DirEntry, error) { return nil, os.ErrNotExist }
	}
	return processReadDir
}
