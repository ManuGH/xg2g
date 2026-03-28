// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import "os"

var (
	processLookupEnv envLookupFunc       = os.LookupEnv
	processGetEnv    func(string) string = os.Getenv
	processEnviron   func() []string     = os.Environ
)

func currentProcessLookupEnv() envLookupFunc {
	if processLookupEnv == nil {
		return func(string) (string, bool) { return "", false }
	}
	return processLookupEnv
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
