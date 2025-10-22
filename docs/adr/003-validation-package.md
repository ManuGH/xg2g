# ADR-003: Centralized Validation Package

**Status**: Accepted
**Date**: 2025-01-21
**Deciders**: Development Team
**Technical Story**: Priority 3 Implementation

## Context and Problem Statement

Before centralization, validation logic was scattered across packages:
- `internal/jobs` had custom validation (44 lines)
- `internal/config` had inline validation
- Inconsistent error messages and duplicate code
- No reusable validators for common patterns (URL, port, directory)

This made the codebase harder to maintain and prone to validation inconsistencies.

## Decision Drivers

- **DRY Principle**: Eliminate duplicate validation code
- **Consistency**: Unified error messages across the application
- **Type Safety**: Replace string-based states with typed enums
- **Testability**: Centralized validation is easier to test thoroughly
- **Developer Experience**: Simple, chainable API for validation

## Considered Options

1. **Centralized Validation Package** with chainable API
2. **Struct Tags Validation** (using libraries like `validator`)
3. **Schema Validation** (JSON Schema or similar)
4. **Inline Validation** (status quo)

## Decision Outcome

**Chosen option**: "Centralized Validation Package with Chainable API"

### Rationale

1. **Zero Dependencies**: No external validation libraries needed
2. **Chainable API**: `v.URL(...).Port(...).IsValid()` reads naturally
3. **Error Accumulation**: Collect all validation errors, not just the first
4. **Custom Logic**: Easy to add xg2g-specific validators (e.g., bouquet names)
5. **Type Safety Integration**: Works seamlessly with typed enums

### Positive Consequences

- **Code Reduction**: 44 lines → 13 lines in jobs validation (70% reduction)
- **Consistency**: All validation errors follow same format
- **Testability**: 84.8% test coverage for validation package
- **Reusability**: Validators used across config, jobs, API handlers
- **Better Error Messages**: Field-specific errors with context

### Negative Consequences

- **Learning Curve**: Developers must learn the validation API
- **Abstraction Overhead**: Simple validations now go through the package

## Pros and Cons of the Options

### Centralized Validation Package

- **Good**, because eliminates code duplication
- **Good**, because consistent error messages
- **Good**, because easy to test and maintain
- **Bad**, because adds abstraction layer

### Struct Tags Validation

- **Good**, because declarative (tags on struct fields)
- **Bad**, because requires external dependencies
- **Bad**, because limited flexibility for custom logic
- **Bad**, because harder to accumulate errors

### Schema Validation

- **Good**, because language-agnostic (JSON Schema)
- **Bad**, because overkill for simple validations
- **Bad**, because poor Go integration
- **Bad**, because runtime overhead

### Inline Validation

- **Good**, because simplest (no abstraction)
- **Bad**, because code duplication
- **Bad**, because inconsistent error messages
- **Bad**, because hard to test comprehensively

## Implementation

### Validator API

```go
package validate

// Validator accumulates validation errors
type Validator struct {
    errors []Error
}

// Chainable validation methods
func (v *Validator) URL(field, value string, schemes []string) *Validator
func (v *Validator) Port(field string, port int) *Validator
func (v *Validator) Directory(field, path string, mustExist bool) *Validator
func (v *Validator) Range(field string, value, min, max int) *Validator
func (v *Validator) NotEmpty(field, value string) *Validator
func (v *Validator) OneOf(field, value string, allowed []string) *Validator
func (v *Validator) Custom(field string, validator func(interface{}) error) *Validator

// Check if valid
func (v *Validator) IsValid() bool
func (v *Validator) Error() string  // Implements error interface
```

### Usage Example

**Before (jobs/refresh.go - 44 lines)**:
```go
func validateConfig(cfg Config) error {
    parsedURL, err := url.Parse(cfg.OWIBase)
    if err != nil {
        return fmt.Errorf("invalid OWI base URL: %w", err)
    }
    if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
        return fmt.Errorf("OWI base must use http or https")
    }
    if cfg.StreamPort < 1 || cfg.StreamPort > 65535 {
        return ErrInvalidStreamPort
    }
    // ... 30 more lines
}
```

**After (jobs/refresh.go - 13 lines)**:
```go
func validateConfig(cfg Config) error {
    v := validate.New()
    v.URL("OWIBase", cfg.OWIBase, []string{"http", "https"})
    v.Port("StreamPort", cfg.StreamPort)
    v.Directory("DataDir", cfg.DataDir, false)
    if !v.IsValid() {
        return v
    }
    return nil
}
```

### Error Accumulation

```go
v := validate.New()
v.URL("OWIBase", "invalid", []string{"http"})
v.Port("StreamPort", 99999)

// Returns both errors:
// "validation failed for OWIBase: invalid URL; StreamPort: port must be between 1 and 65535"
```

### Type-Safe Enums Integration

```go
// internal/validate/types.go
type LogLevel string

const (
    LogLevelDebug LogLevel = "debug"
    LogLevelInfo  LogLevel = "info"
    LogLevelWarn  LogLevel = "warn"
    LogLevelError LogLevel = "error"
)

func (l LogLevel) IsValid() bool {
    switch l {
    case LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError:
        return true
    default:
        return false
    }
}
```

### Testing

```go
func TestValidator_URL(t *testing.T) {
    v := validate.New()
    v.URL("OWIBase", "invalid", []string{"http"})

    if v.IsValid() {
        t.Error("Expected validation to fail for invalid URL")
    }

    if !strings.Contains(v.Error(), "OWIBase") {
        t.Error("Error should mention field name")
    }
}
```

## Architecture

```
internal/validate/
├── validate.go       # Core Validator struct and methods
├── types.go          # Type-safe enums (LogLevel, etc.)
└── validate_test.go  # Tests (84.8% coverage)

internal/config/
└── validation.go     # Config validation using validate package

internal/jobs/
└── refresh.go        # Job validation using validate package
```

## Migration Path

**Phase 1 (Completed)**: Create validation package
- Implement core validators
- Add comprehensive tests
- 84.8% test coverage

**Phase 2 (Completed)**: Migrate existing validation
- Config validation (40 lines)
- Jobs validation (13 lines, down from 44)
- API handler validation

**Phase 3 (Future)**: Extend validators
- Add more type-safe enums (JobStatus, StreamState)
- Custom validators for domain-specific logic
- Validation middleware for HTTP handlers

## Links

- Implementation: `internal/validate/`
- Related: ADR-002 (Config Precedence) - validates merged config
- Related: ADR-001 (API Versioning) - version-specific validation rules

## Notes

### Lessons Learned

1. **Error Accumulation Matters**: Users prefer seeing all errors at once, not one-by-one
2. **Chainable API Wins**: `v.URL().Port().IsValid()` is more readable than nested ifs
3. **Type Safety First**: Enums prevent entire classes of validation bugs
4. **Test Coverage is Critical**: 84.8% coverage caught edge cases

### Future Considerations

1. **Async Validation**: For expensive checks (network, database)
2. **Conditional Validation**: If field X is set, validate field Y
3. **Internationalization**: Localized error messages
4. **Auto-Generated Docs**: Extract validation rules for API documentation

### Performance

- **Zero-Allocation Path**: Validation without errors doesn't allocate
- **Benchmark**: 10,000 validations in <1ms (negligible overhead)

### Common Usage Patterns

**Pattern 1**: Validate entire config
```go
func Validate(cfg Config) error {
    v := validate.New()
    v.URL("OWIBase", cfg.OWIBase, []string{"http", "https"})
    v.Port("StreamPort", cfg.StreamPort)
    if cfg.EPGEnabled {
        v.Range("EPGDays", cfg.EPGDays, 1, 14)
    }
    return v.Check() // Returns error or nil
}
```

**Pattern 2**: Custom validators
```go
v.Custom("Bouquet", cfg.Bouquet, func(val interface{}) error {
    bouquet := val.(string)
    if !isValidBouquetName(bouquet) {
        return fmt.Errorf("invalid bouquet name")
    }
    return nil
})
```
