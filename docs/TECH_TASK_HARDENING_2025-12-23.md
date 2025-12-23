# TECHNICAL TASK: Production Hardening Sprint
**Assigned To:** Engineering Team
**Created:** 2025-12-23
**Priority:** HIGH
**Target:** 3 Sprints (6 weeks)
**Current Score:** 7.8/10 → **Target:** 9.0/10

---

## 📋 EXECUTIVE SUMMARY

xg2g has **excellent architecture** (9/10) but requires **critical fixes** before scaling beyond 500 concurrent users. This task addresses:
- 🔴 **3 Critical Security Issues** (Token exposure, error handling)
- 🟠 **4 High-Priority Concurrency Issues** (Lock contention, race conditions)
- 🟡 **6 Medium-Priority Improvements** (Observability, testing)

**Est. Engineering Effort:** 15-20 days (1 senior engineer)

---

## 🎯 SPRINT 1: CRITICAL FIXES (Week 1-2)

### TASK-001: Fix Idempotency Error Handling ⚡ **[BLOCKER]**
**Priority:** CRITICAL
**Effort:** 2 hours
**Impact:** Prevents duplicate streams and data corruption

#### Problem
```go
// File: internal/api/handlers_v3.go:51-53
if err != nil {
    log.L().Error().Err(err).Msg("idempotency check failed")
}
// Continues execution even if store is unavailable!
sessionID := uuid.New().String()
```

#### Solution
```go
if err != nil {
    log.L().Error().Err(err).Str("idem_key", req.IdempotencyKey).Msg("idempotency check failed")
    http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)
    return
}
```

#### Test Plan
1. Add test case: `TestStreamStart_IdempotencyStoreFailure`
2. Mock store to return error
3. Assert HTTP 503 response
4. Verify no session created in store

#### Files to Modify
- `internal/api/handlers_v3.go` (lines 51-53)
- `internal/api/handlers_v3_test.go` (new test)

---

### TASK-002: Remove Query Parameter Token Support 🔐 **[SECURITY]**
**Priority:** CRITICAL
**Effort:** 4 hours
**Impact:** Prevents token leakage in logs and browser history

#### Problem
```go
// File: internal/api/http.go:609-610
// For streaming, we ALLOW query parameter tokens (legacy or direct stream links)
reqToken := extractToken(r, true)  // allowQuery=true
```

**Risk:** OAuth2/OIDC violation - tokens appear in:
- Proxy server access logs
- Browser history
- Referrer headers

#### Solution (3-Phase Rollout)

**Phase 1: Add Deprecation Warning (Week 1)**
```go
func extractToken(r *http.Request, allowQuery bool) string {
    // Try Authorization header first
    if authHeader := r.Header.Get("Authorization"); authHeader != "" {
        if strings.HasPrefix(authHeader, "Bearer ") {
            return strings.TrimPrefix(authHeader, "Bearer ")
        }
    }

    // Check query param with deprecation warning
    if allowQuery {
        if token := r.URL.Query().Get("token"); token != "" {
            log.L().Warn().
                Str("path", r.URL.Path).
                Str("remote_addr", r.RemoteAddr).
                Msg("DEPRECATED: Query parameter authentication will be removed in v3.0")
            return token
        }
    }

    // Cookie fallback
    if cookie, err := r.Cookie("xg2g_session"); err == nil {
        return cookie.Value
    }

    return ""
}
```

**Phase 2: Add Config Flag (Week 1)**
```go
// File: internal/config/config.go
type APIConfig struct {
    // ...existing fields...
    AllowQueryTokens bool `yaml:"allow_query_tokens" env:"XG2G_ALLOW_QUERY_TOKENS" default:"false"`
}
```

**Phase 3: Update Documentation (Week 1)**
- Update README.md with migration guide
- Add to CHANGELOG.md as breaking change
- Update API documentation

#### Test Plan
1. Test Authorization header auth (should work)
2. Test query param auth with flag=false (should fail 401)
3. Test query param auth with flag=true (should warn + work)
4. Test cookie auth (should work)

#### Files to Modify
- `internal/api/http.go` (lines 609-610, extractToken function)
- `internal/config/config.go` (add flag)
- `internal/api/http_test.go` (add tests)
- `README.md` (migration guide)
- `CHANGELOG.md` (breaking change notice)

---

### TASK-003: Fix MemoryStore Lock Contention 🔒 **[CRITICAL]**
**Priority:** CRITICAL
**Effort:** 6 hours
**Impact:** Prevents blocking all reads during slow callbacks

#### Problem
```go
// File: internal/v3/store/memory_store.go:179-194
func (m *MemoryStore) ScanSessions(ctx context.Context, fn func(*model.SessionRecord) error) error {
    m.mu.RLock()
    defer m.mu.RUnlock()

    for _, rec := range m.sessions {
        if err := fn(&cpy); err != nil {  // Callback holds RLock!
            return err
        }
    }
}
```

**Impact:** If callback is slow (network I/O, expensive computation), ALL read operations are blocked.

#### Solution
```go
func (m *MemoryStore) ScanSessions(ctx context.Context, fn func(*model.SessionRecord) error) error {
    // Step 1: Create snapshot under lock
    m.mu.RLock()
    snapshot := make([]*model.SessionRecord, 0, len(m.sessions))
    for _, rec := range m.sessions {
        cpy := *rec  // Deep copy
        snapshot = append(snapshot, &cpy)
    }
    m.mu.RUnlock()

    // Step 2: Iterate without lock
    for _, rec := range snapshot {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }

        if err := fn(rec); err != nil {
            return err
        }
    }

    return nil
}
```

#### Test Plan
1. Add benchmark: `BenchmarkMemoryStore_ScanSessions_Concurrent`
2. Simulate slow callback (100ms sleep)
3. Run concurrent GetSession operations
4. Assert no blocking (latency <10ms)

#### Files to Modify
- `internal/v3/store/memory_store.go` (lines 179-194, 235-250)
- `internal/v3/store/memory_store_test.go` (add benchmark)
- Apply same pattern to `ScanPipelines` method

---

### TASK-004: Replace Sleep with Readiness Check 🚦 **[RELIABILITY]**
**Priority:** HIGH
**Effort:** 4 hours
**Impact:** Eliminates startup race conditions

#### Problem
```go
// File: internal/daemon/manager.go:277-278
// Give proxy a moment to start listening
time.Sleep(100 * time.Millisecond)  // Hard-coded sleep!
```

**Risk:** On slow systems (CI, loaded servers), 100ms may be insufficient.

#### Solution
```go
// File: internal/daemon/manager.go

func (m *Manager) waitForProxyReady(ctx context.Context, maxWait time.Duration) error {
    proxyURL := fmt.Sprintf("http://%s/health", m.proxyCfg.ListenAddr)

    ticker := time.NewTicker(10 * time.Millisecond)
    defer ticker.Stop()

    timeout := time.After(maxWait)

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-timeout:
            return fmt.Errorf("proxy readiness timeout after %v", maxWait)
        case <-ticker.C:
            resp, err := http.Get(proxyURL)
            if err == nil {
                resp.Body.Close()
                if resp.StatusCode == http.StatusOK {
                    m.logger.Debug().Msg("proxy ready")
                    return nil
                }
            }
        }
    }
}

// Replace in StartProxy:
if err := m.waitForProxyReady(ctx, 5*time.Second); err != nil {
    return fmt.Errorf("proxy startup failed: %w", err)
}
```

#### Test Plan
1. Test successful startup (proxy starts within 5s)
2. Test timeout (proxy never starts)
3. Test context cancellation during wait

#### Files to Modify
- `internal/daemon/manager.go` (lines 277-278, add waitForProxyReady)
- `internal/daemon/manager_test.go` (add tests)

---

## 🟠 SPRINT 2: HIGH-PRIORITY IMPROVEMENTS (Week 3-4)

### TASK-005: Add CIDR Support to Rate Limiting 🌐
**Priority:** HIGH
**Effort:** 3 hours
**Impact:** Allows whitelisting entire subnets

#### Problem
```go
// File: internal/api/middleware/ratelimit.go:68
for _, allowed := range cfg.Whitelist {
    if allowed == ip { // TODO: Support CIDR
        next.ServeHTTP(w, r)
        return
    }
}
```

#### Solution
```go
import "net"

type RateLimitConfig struct {
    // ...existing fields...
    Whitelist     []string       // Now supports CIDR notation
    whitelistNets []*net.IPNet   // Parsed CIDR ranges (internal)
}

func (cfg *RateLimitConfig) parseWhitelist() error {
    for _, entry := range cfg.Whitelist {
        // Try parsing as CIDR
        _, ipNet, err := net.ParseCIDR(entry)
        if err != nil {
            // Try as plain IP
            ip := net.ParseIP(entry)
            if ip == nil {
                return fmt.Errorf("invalid IP/CIDR: %s", entry)
            }
            // Convert single IP to /32 or /128 CIDR
            mask := net.CIDRMask(32, 32)
            if ip.To4() == nil {
                mask = net.CIDRMask(128, 128)
            }
            ipNet = &net.IPNet{IP: ip, Mask: mask}
        }
        cfg.whitelistNets = append(cfg.whitelistNets, ipNet)
    }
    return nil
}

func (cfg *RateLimitConfig) isWhitelisted(ipStr string) bool {
    ip := net.ParseIP(ipStr)
    if ip == nil {
        return false
    }

    for _, ipNet := range cfg.whitelistNets {
        if ipNet.Contains(ip) {
            return true
        }
    }
    return false
}
```

#### Test Plan
1. Test single IP: `192.168.1.100`
2. Test CIDR: `10.0.0.0/8`, `192.168.0.0/16`
3. Test IPv6: `2001:db8::/32`
4. Test invalid input

#### Files to Modify
- `internal/api/middleware/ratelimit.go` (add CIDR parsing)
- `internal/api/middleware/ratelimit_test.go` (add CIDR tests)
- `docs/guides/CONFIGURATION.md` (document CIDR syntax)

---

### TASK-006: Fix JSON Encoding Error Handling 📝
**Priority:** HIGH
**Effort:** 2 hours
**Impact:** Prevents silent response corruption

#### Problem
```go
// File: internal/api/handlers_v3.go:58, 147, 182
_ = json.NewEncoder(w).Encode(...)  // Errors ignored!
```

#### Solution
```go
func writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(statusCode)

    if err := json.NewEncoder(w).Encode(data); err != nil {
        // Headers already sent, can't change status code
        // Log error for debugging
        log.L().Error().
            Err(err).
            Int("status", statusCode).
            Msg("failed to encode JSON response - client may receive partial data")
    }
}

// Usage:
writeJSON(w, http.StatusOK, map[string]string{"sessionId": sessionID})
```

#### Files to Modify
- `internal/api/helpers.go` (new file, add writeJSON helper)
- `internal/api/handlers_v3.go` (replace all `_ = json.NewEncoder`)
- `internal/api/handlers.go` (replace all `_ = json.NewEncoder`)

---

### TASK-007: Add Circuit Breaker Metrics 📊
**Priority:** HIGH
**Effort:** 3 hours
**Impact:** Enables monitoring of circuit breaker state

#### Problem
Circuit breaker exists but state changes not observable via metrics.

#### Solution
```go
// File: internal/api/circuit_breaker.go

var (
    circuitBreakerState = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "xg2g_circuit_breaker_state",
            Help: "Circuit breaker state (0=closed, 1=half-open, 2=open)",
        },
        []string{"name"},
    )

    circuitBreakerTrips = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "xg2g_circuit_breaker_trips_total",
            Help: "Total number of circuit breaker trips",
        },
        []string{"name", "reason"},
    )
)

func (cb *CircuitBreaker) setState(newState string) {
    cb.mu.Lock()
    defer cb.mu.Unlock()

    oldState := cb.state
    cb.state = newState

    // Update metrics
    stateValue := 0.0
    switch newState {
    case "closed":
        stateValue = 0.0
    case "half-open":
        stateValue = 1.0
    case "open":
        stateValue = 2.0
        circuitBreakerTrips.WithLabelValues(cb.name, "threshold_exceeded").Inc()
    }
    circuitBreakerState.WithLabelValues(cb.name).Set(stateValue)

    log.L().Info().
        Str("name", cb.name).
        Str("old_state", oldState).
        Str("new_state", newState).
        Msg("circuit breaker state changed")
}
```

#### Alerts to Create
```yaml
# alerts.yml
- alert: CircuitBreakerOpen
  expr: xg2g_circuit_breaker_state{name="enigma2"} == 2
  for: 1m
  annotations:
    summary: "Circuit breaker {{ $labels.name }} is OPEN"
    description: "Upstream service is failing, requests are being rejected"
```

#### Files to Modify
- `internal/api/circuit_breaker.go` (add metrics)
- `internal/metrics/metrics.go` (register metrics)
- `docs/OBSERVABILITY.md` (document metrics)

---

### TASK-008: Implement Mandatory Audit Logging 🔍
**Priority:** MEDIUM
**Effort:** 4 hours
**Impact:** Ensures security events are never lost

#### Problem
```go
// File: internal/api/http.go:636-638
if s.auditLogger != nil {
    s.auditLogger.AuthSuccess(clientIP(r), r.URL.Path)
}
// If auditLogger is nil, auth events are lost!
```

#### Solution
```go
// File: internal/api/audit.go (new file)
package api

type AuditEvent struct {
    Timestamp   time.Time         `json:"timestamp"`
    EventType   string            `json:"event_type"`
    ClientIP    string            `json:"client_ip"`
    UserAgent   string            `json:"user_agent"`
    Path        string            `json:"path"`
    Outcome     string            `json:"outcome"`
    Metadata    map[string]string `json:"metadata,omitempty"`
}

type AuditLogger interface {
    LogEvent(event AuditEvent)
}

// DefaultAuditLogger writes to structured log
type DefaultAuditLogger struct{}

func (l *DefaultAuditLogger) LogEvent(e AuditEvent) {
    log.L().Info().
        Time("ts", e.Timestamp).
        Str("event", e.EventType).
        Str("client_ip", e.ClientIP).
        Str("path", e.Path).
        Str("outcome", e.Outcome).
        Msg("audit_event")
}

// In Server initialization:
if s.auditLogger == nil {
    s.auditLogger = &DefaultAuditLogger{}
}
```

#### Events to Audit
- `auth_success` - Successful authentication
- `auth_failure` - Failed authentication
- `session_start` - Stream session created
- `session_stop` - Stream session terminated
- `config_reload` - Configuration reloaded
- `admin_action` - Admin API calls

#### Files to Modify
- `internal/api/audit.go` (new file)
- `internal/api/http.go` (use default logger if nil)
- `internal/api/handlers_v3.go` (add audit calls)
- `docs/SECURITY.md` (document audit events)

---

## 🟡 SPRINT 3: OBSERVABILITY & TESTING (Week 5-6)

### TASK-009: Add V3 Request Latency Metrics 📈
**Priority:** MEDIUM
**Effort:** 3 hours

#### Solution
```go
// File: internal/api/middleware/metrics.go
var v3RequestDuration = promauto.NewHistogramVec(
    prometheus.HistogramOpts{
        Name:    "xg2g_v3_http_request_duration_seconds",
        Help:    "V3 API request latency",
        Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5},
    },
    []string{"method", "path", "status"},
)

func V3MetricsMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if !strings.HasPrefix(r.URL.Path, "/v3/") {
            next.ServeHTTP(w, r)
            return
        }

        start := time.Now()
        ww := &statusWriter{ResponseWriter: w, statusCode: 200}

        next.ServeHTTP(ww, r)

        duration := time.Since(start).Seconds()
        v3RequestDuration.WithLabelValues(
            r.Method,
            r.URL.Path,
            strconv.Itoa(ww.statusCode),
        ).Observe(duration)
    })
}
```

#### Files to Modify
- `internal/api/middleware/metrics.go` (add middleware)
- `internal/api/http.go` (register middleware)

---

### TASK-010: Increase V3 Test Coverage 🧪
**Priority:** MEDIUM
**Effort:** 8 hours
**Target:** 46% → 75% coverage

#### Current Gap
- **16 test files** for **35 implementation files** = 46% coverage
- Missing: Concurrent intent processing, store failures, bus errors

#### Test Cases to Add

**1. Concurrency Tests**
```go
// File: internal/v3/worker/orchestrator_concurrent_test.go
func TestOrchestrator_ConcurrentIntents_SameService(t *testing.T) {
    // Test: 10 concurrent start intents for same serviceRef
    // Expected: Only 1 lease acquired, others get R_LEASE_BUSY
}

func TestOrchestrator_ConcurrentIntents_DifferentServices(t *testing.T) {
    // Test: 10 concurrent intents for different services
    // Expected: All succeed with different tuner slots
}
```

**2. Error Injection Tests**
```go
// File: internal/v3/store/badger_store_test.go
func TestBadgerStore_GetSession_DBClosed(t *testing.T) {
    // Test: Query after db.Close()
    // Expected: Error returned, no panic
}

func TestBadgerStore_UpdateSession_ConcurrentModification(t *testing.T) {
    // Test: Two goroutines update same session
    // Expected: One succeeds, one gets conflict error
}
```

**3. Bus Failure Tests**
```go
// File: internal/v3/bus/memory_bus_test.go
func TestBus_Publish_NoSubscribers(t *testing.T) {
    // Test: Publish to topic with no subscribers
    // Expected: No error, event dropped
}

func TestBus_Subscribe_AfterClose(t *testing.T) {
    // Test: Subscribe after bus closed
    // Expected: Error returned
}
```

#### Files to Create/Modify
- `internal/v3/worker/orchestrator_concurrent_test.go` (new)
- `internal/v3/store/badger_store_test.go` (expand)
- `internal/v3/bus/memory_bus_test.go` (expand)
- `internal/v3/api/handlers_v3_test.go` (add error cases)

---

### TASK-011: Add TLS Certificate Validation 🔐
**Priority:** MEDIUM
**Effort:** 2 hours

#### Problem
```go
// File: internal/daemon/manager.go:162-175
if tlsCert != "" && tlsKey != "" {
    // No validation if files exist or are valid!
    m.apiServer.ListenAndServeTLS(tlsCert, tlsKey)
}
```

#### Solution
```go
// File: internal/validation/tls.go (new file)
func ValidateTLSCertificate(certPath, keyPath string) error {
    // Check files exist
    if _, err := os.Stat(certPath); err != nil {
        return fmt.Errorf("cert file not found: %w", err)
    }
    if _, err := os.Stat(keyPath); err != nil {
        return fmt.Errorf("key file not found: %w", err)
    }

    // Try loading certificate
    cert, err := tls.LoadX509KeyPair(certPath, keyPath)
    if err != nil {
        return fmt.Errorf("invalid TLS certificate: %w", err)
    }

    // Parse certificate for validation
    x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
    if err != nil {
        return fmt.Errorf("failed to parse certificate: %w", err)
    }

    // Warn if expiring soon
    if time.Until(x509Cert.NotAfter) < 30*24*time.Hour {
        log.L().Warn().
            Time("expires_at", x509Cert.NotAfter).
            Msg("TLS certificate expires in less than 30 days")
    }

    // Warn if already expired
    if time.Now().After(x509Cert.NotAfter) {
        return fmt.Errorf("TLS certificate expired on %v", x509Cert.NotAfter)
    }

    return nil
}

// In manager.go startup:
if tlsCert != "" && tlsKey != "" {
    if err := validation.ValidateTLSCertificate(tlsCert, tlsKey); err != nil {
        return fmt.Errorf("TLS validation failed: %w", err)
    }
    m.logger.Info().
        Str("cert", tlsCert).
        Msg("TLS certificate validated successfully")
}
```

#### Files to Modify
- `internal/validation/tls.go` (new file)
- `internal/daemon/manager.go` (add validation call)
- `internal/validation/tls_test.go` (new tests)

---

### TASK-012: Implement Redis Store for Clustering 🗄️
**Priority:** LOW (Future Sprint)
**Effort:** 3 days
**Impact:** Enables horizontal scaling

#### Design
```go
// File: internal/v3/store/redis_store.go (new file)
type RedisStore struct {
    client *redis.Client
    prefix string  // Key prefix (e.g., "xg2g:v3:")
}

func NewRedisStore(client *redis.Client) *RedisStore {
    return &RedisStore{
        client: client,
        prefix: "xg2g:v3:",
    }
}

// Implement StateStore interface
func (r *RedisStore) PutSession(ctx context.Context, s *model.SessionRecord) error {
    data, err := json.Marshal(s)
    if err != nil {
        return err
    }

    key := r.prefix + "session:" + s.SessionID
    ttl := time.Duration(s.ExpiresAtUnix-time.Now().Unix()) * time.Second

    return r.client.Set(ctx, key, data, ttl).Err()
}

// Lease implementation using Redis SET NX
func (r *RedisStore) TryAcquireLease(ctx context.Context, key, owner string, ttl time.Duration) (Lease, bool, error) {
    leaseKey := r.prefix + "lease:" + key

    // SET NX = Set if Not eXists (atomic)
    success, err := r.client.SetNX(ctx, leaseKey, owner, ttl).Result()
    if err != nil {
        return nil, false, err
    }

    if !success {
        return nil, false, nil  // Lease already held
    }

    return &redisLease{
        key:       key,
        owner:     owner,
        expiresAt: time.Now().Add(ttl),
    }, true, nil
}
```

#### Configuration
```yaml
# config.yaml
v3:
  store:
    type: redis  # memory | bolt | badger | redis
    redis:
      addr: localhost:6379
      password: ""
      db: 0
      pool_size: 10
```

#### Files to Create
- `internal/v3/store/redis_store.go`
- `internal/v3/store/redis_store_test.go`
- `internal/v3/store/factory.go` (update to support Redis)

**Note:** Defer to future sprint as requires Redis dependency and testing.

---

## 📊 SUCCESS METRICS

### Sprint 1 (Critical Fixes)
- [ ] All Critical tests pass
- [ ] No regressions in existing tests
- [ ] Security scan passes (gosec, govulncheck)
- [ ] Load test: 100 concurrent sessions without errors

### Sprint 2 (High Priority)
- [ ] CIDR whitelist tested with production subnet ranges
- [ ] Circuit breaker metrics visible in Grafana
- [ ] Audit log events appear for all security operations

### Sprint 3 (Observability & Testing)
- [ ] V3 test coverage: 46% → 75%
- [ ] All metrics exportable to Prometheus
- [ ] TLS cert validation tested with expired certs

### Final Acceptance
- [ ] Load test: 500 concurrent sessions, <100ms p99 latency
- [ ] No memory leaks after 24h run
- [ ] All TODO comments resolved or converted to issues
- [ ] Documentation updated

---

## 🔧 DEVELOPMENT WORKFLOW

### Setup
```bash
# Clone and setup
git clone https://github.com/ManuGH/xg2g.git
cd xg2g
go mod download

# Run tests with race detector
make test-race

# Run linters
make lint
```

### Testing Each Task
```bash
# Unit tests
go test -v -race ./internal/v3/...

# Integration tests
go test -v -tags=integration ./test/integration/...

# Coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Code Review Checklist
- [ ] Tests added for new code
- [ ] Race detector passes (`-race` flag)
- [ ] Linters pass (`make lint`)
- [ ] Documentation updated
- [ ] CHANGELOG.md updated
- [ ] No TODO comments without issue reference

---

## 📈 ESTIMATED IMPACT

| Metric | Before | After | Improvement |
|--------|--------|-------|-------------|
| **Overall Score** | 7.8/10 | 9.0/10 | +15% |
| **Stability** | 7.5/10 | 9.0/10 | +20% |
| **Concurrency Safety** | 6/10 | 9/10 | +50% |
| **Observability** | 7/10 | 8.5/10 | +21% |
| **Test Coverage (V3)** | 46% | 75% | +63% |
| **Max Concurrent Users** | ~500 | ~2000 | +300% |

---

## 🚨 ROLLBACK PLAN

If any task causes production issues:

1. **Immediate Rollback**
   ```bash
   git revert <commit-hash>
   make build
   systemctl restart xg2g
   ```

2. **Feature Flags**
   - All breaking changes behind config flags
   - Can disable via environment variable
   - Example: `XG2G_ALLOW_QUERY_TOKENS=true` (re-enable old behavior)

3. **Monitoring**
   - Alert on error rate spike (>1% errors)
   - Alert on latency spike (p99 >500ms)
   - Alert on memory growth (>2GB)

---

## 📚 REFERENCES

- [Production Readiness Review](./PRODUCTION_READINESS_REVIEW_2025-12-18.md)
- [Architecture Decision Records](./adr/)
- [Security Invariants](./SECURITY_INVARIANTS.md)
- [Observability Guide](./OBSERVABILITY.md)

---

## ✅ SIGN-OFF

**Prepared By:** AI Code Analysis
**Reviewed By:** _[Engineering Lead]_
**Approved By:** _[Tech Lead]_
**Date:** 2025-12-23

---

**Questions?** Open a discussion: https://github.com/ManuGH/xg2g/discussions
