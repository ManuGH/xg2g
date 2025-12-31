//go:build !v3_idempotency
// +build !v3_idempotency

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

// Test-only compile-time toggle for Phase-2 acceptance tests.
const v3IdempotencyEnabled = false
