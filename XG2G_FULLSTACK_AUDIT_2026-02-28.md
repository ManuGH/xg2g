# xg2g Full-Stack Audit (2026-02-28)

## Phase 1 — Security Audit
| ID | CVSS | Vector | Datei:Zeile | Finding | Exploitation | Fix |
|---|---:|---|---|---|---|---|
| SEC-01 | 8.2 | AV:A/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:N | `internal/config/registry.go:124`, `internal/config/registry.go:176`, `internal/config/registry.go:179`, `internal/daemon/manager.go:182`, `internal/daemon/manager.go:186`, `internal/control/http/v3/auth.go:116` | Default deployment path is cleartext HTTP (`:8088`), TLS/ForceHTTPS default off, session cookie `Secure` only when TLS/ForceHTTPS. | Attacker on same WLAN/L2 can sniff Bearer/cookie token and replay privileged API calls. | Set secure defaults (`TLSEnabled=true`, `ForceHTTPS=true`), fail `CreateSession` on non-TLS, add HSTS. |
| SEC-02 | 7.1 | AV:L/AC:L/PR:L/UI:N/S:U/C:H/I:H/A:N | `internal/pipeline/exec/enigma2/client_ext.go:85`, `internal/pipeline/exec/enigma2/client_ext.go:89`, `internal/pipeline/exec/enigma2/client_ext.go:130`, `internal/pipeline/exec/enigma2/client_ext.go:135` | Stream URL logs include userinfo credentials (`user:pass@host`). | Any log reader gets reusable OpenWebIF credentials. | Replace raw URL logging with redacted helper (`u.User=nil`) before logging. |
| SEC-03 | 6.5 | AV:N/AC:L/PR:N/UI:R/S:U/C:H/I:H/A:N | `internal/control/http/v3/auth.go:111`, `internal/control/http/v3/auth.go:113`, `internal/control/auth/token.go:55`, `internal/control/auth/token.go:57` | Session cookie value is the raw API bearer token. | Cookie theft equals full API token theft; no server-side session revocation granularity. | Use opaque session IDs in cookie + server-side session store mapping/token hash + revoke endpoint. |
| SEC-04 | 5.3 | AV:A/AC:L/PR:N/UI:N/S:U/C:L/I:N/A:N | `internal/app/bootstrap/bootstrap.go:279`, `internal/app/bootstrap/bootstrap.go:283`, `internal/daemon/manager.go:208`, `internal/daemon/manager.go:210` | Metrics server is unauthenticated and falls back to `:9090` when enabled. | Network-local attacker can scrape runtime internals/traffic metadata. | Default bind to `127.0.0.1:9090`, protect with auth/mTLS, or enforce firewalling. |
| SEC-05 | 4.3 | AV:N/AC:L/PR:N/UI:R/S:U/C:L/I:L/A:N | `internal/config/registry.go:133`, `internal/control/auth/token.go:64`, `internal/control/auth/token.go:70` | Legacy token sources are enabled by default (`X-API-Token` header/cookie). | Expands accepted credential channels and keeps downgrade path alive. | Flip default (`api.disableLegacyTokenSources=true`) and remove legacy extraction path. |
| SEC-06 | 3.7 | AV:N/AC:L/PR:N/UI:R/S:U/C:L/I:L/A:N | `internal/control/http/v3/recordings_resume.go:23`, `internal/control/http/v3/recordings_resume.go:33` | Resume endpoint hardcodes `Access-Control-Allow-Origin: *` and bypasses central CORS policy. | Any origin can perform preflight/requests when token is present client-side. | Remove per-handler CORS headers; rely on middleware allowlist (`internal/control/middleware/cors.go`). |

## Phase 2 — Architektur & Concurrency
| ID | Severity | Component | Datei:Zeile | Finding | Fix | Effort |
|---|---|---|---|---|---|---|
| CON-04 | HIGH | `watchdog` | `internal/media/ffmpeg/watchdog/watchdog.go:149`, `internal/media/ffmpeg/watchdog/watchdog.go:159`, `internal/media/ffmpeg/watchdog/watchdog.go:164` | `check()` takes `RLock` but mutates `w.state`; race-prone shared state mutation under read lock. | Use full `Lock` for state transitions or atomics + immutable read path. | S |
| CON-05 | HIGH | `ffmpeg lifecycle` | `internal/infra/media/ffmpeg/adapter.go:316`, `internal/infra/media/ffmpeg/adapter.go:328`, `internal/infra/media/ffmpeg/adapter.go:331`, `internal/infra/ffmpeg/runner.go:137`, `internal/infra/ffmpeg/runner.go:155`, `internal/infra/ffmpeg/runner.go:158` | Watchdog timeout is checked too late (after scanner/`Wait`); stalled process may outlive timeout policy. | Split stderr scan into goroutine; `select` on `wdErrCh`/`procWait`; on watchdog timeout call `procgroup.KillGroup`. | M |
| CON-06 | MEDIUM | `scan manager` | `internal/pipeline/scan/manager.go:139`, `internal/pipeline/scan/manager.go:141`, `internal/pipeline/scan/manager.go:147` | Background scans run on `context.Background()` (30m timeout), detached from daemon shutdown hooks. | Add manager-owned context+cancel+waitgroup; stop/join via daemon shutdown hook. | M |
| ERR-01 | MEDIUM | `openwebif error mapping` | `internal/openwebif/timer_errors.go:9`, `internal/openwebif/timer_errors.go:10`, `internal/openwebif/timer_errors.go:68`, `internal/openwebif/timer_errors.go:74` | Error semantics still depend on localized string token matching (`"conflict"`, `"Konflikt"`, `"404"`). | Replace with typed error mapping from structured upstream status/code payloads; use `errors.Is/As` at call sites. | M |
| CON-07 | LOW | `recordings resolver` | `internal/control/recordings/resolver.go:242`, `internal/control/recordings/resolver.go:244`, `internal/control/recordings/resolver.go:284` | Detached probe goroutine uses background timeout and silently drops `SetDuration` errors. | Thread runtime context into probe task + log/metric on duration-store write failure. | S |
| ARC-01 | LOW | `package boundaries` | `internal/control/http/v3/server.go:1` | `go list` import fan-out remains high (`internal/control/http/v3=77`, `internal/api=48`). | Split by bounded contexts (auth/session/recordings/system), invert dependencies behind narrow interfaces. | L |

## Phase 3 — Transcoding Pipeline
| ID | Severity | Symptom | Datei:Zeile | Root Cause | Fix | Test-Strategie |
|---|---|---|---|---|---|---|
| TRN-01 | HIGH | Stalled FFmpeg sessions exceed configured stall timeout. | `internal/infra/media/ffmpeg/adapter.go:316`, `internal/infra/media/ffmpeg/adapter.go:328`, `internal/infra/media/ffmpeg/adapter.go:331`, `internal/infra/ffmpeg/runner.go:155`, `internal/infra/ffmpeg/runner.go:158` | Watchdog result is consumed only after scanner/`Wait`; no immediate kill path on watchdog failure. | Monitor process and watchdog concurrently; terminate procgroup immediately on watchdog timeout. | Integration test with FFmpeg fixture emitting no progress; assert process killed `< stallTimeout + killGrace`. |
| TRN-02 | MEDIUM | Client disconnects can still be counted as playback success. | `internal/control/http/v3/recordings.go:274`, `internal/control/http/v3/recordings.go:276`, `internal/control/http/v3/recordings.go:333`, `internal/control/http/v3/recordings.go:335`, `internal/control/http/v3/recordings_hls.go:196`, `internal/control/http/v3/recordings_hls.go:250` | `io.Copy`/`io.CopyN` write errors are ignored; success metrics are emitted unconditionally after copy call. | Check copy errors, classify `context.Canceled`/broken pipe as aborted, suppress success SLO marks. | Integration test that cancels HTTP client mid-stream and asserts no success outcome metric. |
| TRN-03 | MEDIUM | VAAPI may stay "ready" after runtime GPU failure/hot-unplug. | `internal/pipeline/hardware/gpu.go:45`, `internal/pipeline/hardware/gpu.go:73`, `internal/app/bootstrap/wiring_helpers.go:69`, `internal/app/bootstrap/wiring_helpers.go:74` | GPU preflight is startup-only cached state; no periodic/runtime demotion after failures. | Add runtime revalidation and failure demotion path tied to encode errors/device checks. | Simulate VAAPI failure after startup (mock/preflight flip); assert fallback to CPU profile path. |
| TRN-04 | MEDIUM | Stream URL resolution leaks credentials into logs. | `internal/pipeline/exec/enigma2/client_ext.go:89`, `internal/pipeline/exec/enigma2/client_ext.go:135` | Logging raw resolved URLs includes userinfo when credentials injected. | Redact userinfo from all URL logs; add redaction unit tests for both direct and resolved paths. | Unit test validates logged fields never contain `@`-userinfo secrets. |

## Phase 4 — Code Quality & Testing
| ID | Severity | File:Line | Finding | Fix |
|---|---|---|---|---|
| QLT-01 | MEDIUM | `coverage.out` | `go test -coverprofile=coverage.out ./...` succeeded with `GOTOOLCHAIN=go1.25.7`; total coverage is `52.7%`. Lowest set includes many `0.0%` command paths (`cmd/configgen/main.go:*`). | Add tests for operational CLI paths or move generator code behind testable package APIs; gate minimum package coverage for `cmd/daemon` and critical handlers. |
| QLT-02 | MEDIUM | `.golangci.yml:7` | `golangci-lint run ./...` exits with `context loading failed: no go files to analyze`; lint gate is currently non-actionable in this environment. | Pin lint+Go versions in `Makefile`/CI wrapper and enforce deterministic invocation (`GOTOOLCHAIN`, module root checks). |
| QLT-03 | MEDIUM | `internal/config/validation.go:69`, `internal/control/http/v3/handlers_intents.go:41`, `internal/app/bootstrap/bootstrap.go:58` | Cyclomatic complexity hotspots remain severe (`86`, `79`, `51` etc. from `gocyclo -over 15`). | Split giant functions into pure validation/routing subfunctions; add table-driven tests per branch cluster. |
| QLT-04 | LOW | `internal/api/._auth.go:1` | 96 AppleDouble `._*.go` files exist under `internal/`; they break parsers (`illegal character NUL`) for tools like `gocyclo`. | Remove `._*` files from repository and ignore them in VCS (`.gitignore`). |
| QLT-05 | LOW | `internal/platform/paths/hls.go:76` | Non-test debt markers (`TODO/FIXME/HACK/XXX/DEPRECATED`) remain in production paths (9 hits from grep run). | Convert active markers into tracked issues and remove stale markers during refactors. |

## Phase 5 — Zusammenfassung
### Status Bekannte Offene Issues
| ID | Status | Evidence |
|---|---|---|
| CON-03 | FIXED | `internal/pipeline/bus/memory_bus.go:53-67` has no `default` drop path; publish blocks or returns on `ctx.Done()`. |
| ERR-03 | FIXED | `cmd/daemon/main.go` currently 177 lines; prior `~416` swallow path no longer exists. |
| CON-01/02 | FIXED | `internal/control/vod/manager.go:26-27`, `:70-71`, `:649-653` joins worker/build goroutines on shutdown. |
| CFG-01 | FIXED | `grep os.Getenv/os.LookupEnv` in `cmd/daemon/main.go` returned none; ENV reads are in config layer. |
| DG-02 | OPEN | `go list` fan-out still high (`internal/control/http/v3=77`, `internal/api=48`). |
| ERR-01 | OPEN | `internal/openwebif/timer_errors.go:9-10,68-75` still uses string token matching. |
| CFG-04 | FIXED | `internal/control/http/v3/server.go:327-330` closes library store during shutdown. |
| ERR-05 | FIXED | `internal/control/authz/policy.go:81-85` no panic; unknown op returns empty list. |

### 1. TOP-10 CRITICAL/HIGH (sofort fixen)
| Rang | ID | Severity | Datei:Zeile | One-Liner | Fix-Aufwand |
|---:|---|---|---|---|---|
| 1 | CON-05 | HIGH | `internal/infra/media/ffmpeg/adapter.go:316` | Watchdog timeout does not deterministically terminate stalled FFmpeg processes. | M |
| 2 | CON-04 | HIGH | `internal/media/ffmpeg/watchdog/watchdog.go:149` | Watchdog mutates shared state under `RLock` (race-prone concurrency bug). | S |
| 3 | SEC-01 | HIGH | `internal/config/registry.go:176` | Secure transport is off by default; token auth can run cleartext on LAN. | M |
| 4 | SEC-02 | HIGH | `internal/pipeline/exec/enigma2/client_ext.go:89` | Stream URL logs leak OpenWebIF credentials. | S |

### 2. MEDIUM (nächste Iteration)
| Rang | ID | Severity | Datei:Zeile | One-Liner | Fix-Aufwand |
|---:|---|---|---|---|---|
| 1 | TRN-02 | MEDIUM | `internal/control/http/v3/recordings.go:274` | Disconnect/write errors are ignored while playback success metrics are still emitted. | M |
| 2 | TRN-03 | MEDIUM | `internal/pipeline/hardware/gpu.go:73` | VAAPI readiness is startup-cached and not revalidated at runtime. | M |
| 3 | SEC-03 | MEDIUM | `internal/control/http/v3/auth.go:113` | Session cookie stores raw bearer token instead of opaque server-side session ID. | M |
| 4 | SEC-04 | MEDIUM | `internal/app/bootstrap/bootstrap.go:283` | Metrics endpoint can expose internals on `:9090` without auth when enabled. | S |
| 5 | ERR-01 | MEDIUM | `internal/openwebif/timer_errors.go:68` | Timer error handling still depends on localized string matching. | M |
| 6 | CON-06 | MEDIUM | `internal/pipeline/scan/manager.go:141` | Background scan lifecycle is detached from shutdown context. | M |
| 7 | QLT-02 | MEDIUM | `.golangci.yml:7` | Lint command currently not actionable (`no go files to analyze`). | S |
| 8 | QLT-01 | MEDIUM | `coverage.out` | Global coverage remains low (`52.7%`) with many untested CLI codepaths. | M |

### 3. LOW / NICE-TO-HAVE
| Rang | ID | Datei:Zeile | One-Liner |
|---:|---|---|---|
| 1 | SEC-05 | `internal/config/registry.go:133` | Legacy token vectors remain enabled by default. |
| 2 | SEC-06 | `internal/control/http/v3/recordings_resume.go:23` | Resume route bypasses central CORS policy with wildcard origin header. |
| 3 | CON-07 | `internal/control/recordings/resolver.go:284` | Background duration persistence errors are silently dropped. |
| 4 | ARC-01 | `internal/control/http/v3/server.go:1` | `control/http/v3` remains a high fan-out package (77 imports). |
| 5 | QLT-04 | `internal/api/._auth.go:1` | AppleDouble files pollute repo and break static analysis tools. |
| 6 | QLT-05 | `internal/platform/paths/hls.go:76` | Non-test TODO/DEPRECATED markers remain in active code paths. |

### 4. SPRINT-PLAN
| Woche | Fokus | Umsetzung |
|---|---|---|
| 1 | Security (Auth, Injection, Secrets) | Fix `SEC-01`, `SEC-02`, `SEC-03`, `SEC-05`, `SEC-06`; add regression tests for URL-redaction + HTTPS-only session creation. |
| 2 | Stability (Goroutine Lifecycle, EventBus, Shutdown) | Fix `CON-04`, `CON-05`, `CON-06`; add lifecycle tests for watchdog-triggered kill and scan shutdown join. |
| 3 | Config + Error Handling Cleanup | Eliminate `ERR-01` string matching; harden lint/toolchain reproducibility (`QLT-02`). |
| 4 | Structure + Coverage | Start `DG-02` split plan (`ARC-01`); raise coverage in critical command/API paths (`QLT-01`). |

### 5. Validierungs-Kommandos (nach jedem Sprint)
| # | Command |
|---:|---|
| 1 | `go test -race ./...` |
| 2 | `GOTOOLCHAIN=go1.25.7 go run golang.org/x/vuln/cmd/govulncheck@latest ./...` |
| 3 | `golangci-lint run ./...` |
| 4 | `grep -rn "context.Background()" internal/ --include="*.go" | grep -v _test.go | wc -l` |
| 5 | `grep -rn "os.Getenv" internal/ cmd/ --include="*.go" | grep -v _test.go | wc -l` |
| 6 | `grep -rn '"broken pipe"\|"connection reset"\|"Konflikt"' internal/ --include="*.go" | wc -l` |
