# Stability & Retry Requirements

> **Language Policy**: This document follows the project's English-only policy for technical documentation.

## Retry-Parameter (Defaults)

| Parameter | Default Value | Range | Description |
|-----------|---------------|--------|-------------|
| `XG2G_OWI_RETRIES` | `3` | `0-10` | Maximum retry attempts |
| `XG2G_OWI_TIMEOUT_MS` | `10000` | `1000-60000` | Timeout per attempt in milliseconds |
| `XG2G_OWI_BACKOFF_MS` | `500` | `100-5000` | Base backoff delay in milliseconds |
| `XG2G_OWI_MAX_BACKOFF_MS` | `2000` | `500-30000` | Maximum backoff delay in milliseconds |

## Retry Strategy

### Exponential Backoff Formula

```text
delay = min(base_backoff * (2^attempt), max_backoff)
```

### Retry Attempts Timeline

- **Attempt 1**: Immediate (0ms delay)
- **Attempt 2**: 500ms delay  
- **Attempt 3**: 1000ms delay
- **Attempt 4**: 2000ms delay (capped at max_backoff)

### Total Maximum Duration

- **Worst case**: 10s + 0.5s + 10s + 1s + 10s + 2s + 10s = ~43.5s
- **Typical case** (fast failure): ~1-5s

## Retry Decision Matrix

| Error Type | Retry? | Reasoning |
|------------|--------|-----------|
| **Network Timeout** | ✅ Yes | Transient network issue |
| **Connection Refused** | ✅ Yes | Receiver may be restarting |
| **Connection Reset** | ✅ Yes | Network instability |
| **DNS Resolution Error** | ❌ No | Configuration issue |
| **HTTP 5xx (500-599)** | ✅ Yes | Server-side transient error |
| **HTTP 4xx (400-499)** | ❌ No | Client error, won't improve with retry |
| **HTTP 3xx (300-399)** | ❌ No | Redirect handling should be automatic |
| **HTTP 2xx (200-299)** | ❌ No | Success, no retry needed |
| **Invalid JSON Response** | ❌ No | Application-level error |
| **Context Cancelled** | ❌ No | User/system initiated cancellation |

## Configuration Validation Rules

### Fail-Fast Validation

- **Negative values**: Reject with clear error message
- **Zero retries**: Allow (disables retry mechanism)
- **Timeout too low** (<1000ms): Reject as unrealistic
- **Timeout too high** (>60000ms): Reject as excessive
- **Backoff too low** (<100ms): Reject as ineffective
- **Max backoff < base backoff**: Reject as illogical

### Validation Error Messages

```text
Invalid XG2G_OWI_TIMEOUT_MS=500: must be between 1000 and 60000
Invalid XG2G_OWI_RETRIES=-1: must be between 0 and 10
Invalid XG2G_OWI_BACKOFF_MS=50: must be between 100 and 5000
Invalid configuration: XG2G_OWI_MAX_BACKOFF_MS (200) < XG2G_OWI_BACKOFF_MS (500)
```

## Logging Strategy

### Log Levels per Attempt

- **First failure**: `WARN` level
- **Intermediate failures**: `WARN` level with attempt counter
- **Final failure**: `ERROR` level (exhausted retries)
- **Success after retry**: `INFO` level with attempt counter

### Required Log Fields

```json
{
  "component": "openwebif",
  "event": "openwebif.request",
  "operation": "bouquets|services.flat|services.nested",
  "method": "GET",
  "endpoint": "/api/bouquets",
  "host": "10.10.55.57",
  "attempt": 2,
  "total_attempts": 3,
  "elapsed_ms": 1250,
  "duration_ms": 750,
  "status": 502,
  "error": "HTTP 502: Bad Gateway",
  "error_class": "http_5xx",
  "will_retry": true,
  "next_delay_ms": 1000
}
```

## Error Classification

### Error Classes for Metrics/Logging

- `timeout`: Request exceeded timeout
- `connection_error`: Network connection failed
- `http_4xx`: Client error (4xx status codes)
- `http_5xx`: Server error (5xx status codes)  
- `json_decode_error`: Invalid response format
- `context_cancelled`: Operation was cancelled
- `unknown`: Unclassified error

## Architecture Decision: Central Request Function

All OpenWebIF HTTP requests MUST go through a single function:

```go
func (c *Client) doRequestWithRetry(ctx context.Context, req *http.Request) (*http.Response, error)
```

This function handles:

1. Timeout enforcement per attempt
2. Retry logic based on error classification
3. Exponential backoff delays
4. Structured logging with all required fields
5. Context cancellation handling

## Success Criteria

### Functional Requirements

- ✅ All existing functionality preserved
- ✅ Configurable timeout/retry parameters via ENV
- ✅ Exponential backoff with jitter
- ✅ Proper error classification
- ✅ No retry on non-transient errors

### Non-Functional Requirements

- ✅ <1% failure rate in staging with stable network
- ✅ No "stuck refresh" scenarios (max 45s total duration)
- ✅ Clear error messages for configuration issues
- ✅ Structured logging for debugging
- ✅ Graceful degradation under load

### Test Coverage Requirements

- ✅ Unit tests for all retry scenarios
- ✅ Integration tests with simulated network issues
- ✅ Configuration validation tests
- ✅ Timeout behavior verification
- ✅ Log output verification
