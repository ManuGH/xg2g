package auth

// DefaultDecisionSecret is the default HMAC-SHA256 signing key for playbackDecisionToken.
// This is the Single Source of Truth (SSOT) shared between handlers and tests.
//
// In production, this should be overridden via config or env.
// TODO: Migrate to config/env + key rotation (see ADR-SEC-001 for rotation policy).
// For now, it serves as the default until a config-driven key rotation
// mechanism is implemented.
//
//nolint:gosec // Hardcoded default secret; planned for config migration.
var DefaultDecisionSecret = []byte("super-secret-key-123456789012345")
