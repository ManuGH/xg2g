# xg2g Full-Stack Code Review

Date: 2026-03-27

## Scope

- Audited working tree: `/root/xg2g`
- Go module root: `backend/`
- Commands actually executed: `rg`, `go list`, `govulncheck`, `gocyclo`, `golangci-lint`, targeted `nl -ba` / `sed`
- Rust was ignored as requested.

## Command Execution Notes

| Command | Result | Note |
| --- | --- | --- |
| `cd backend && go test -coverprofile=coverage.out ./...` | Failed | Not trustworthy in this environment. Two separate failures were reproduced: `cmd/daemon/report_cmd_test.go:14` opens a localhost listener that is blocked in the sandbox, and repeated runs also hit a mixed local Go toolchain state (`compile version go1.25.7` vs package archives built with `go tool version go1.25.0`). |
| `cd backend && go tool cover -func=coverage.out` | Not run | No trustworthy full `coverage.out` was produced. |

## Phase 1: Security Audit

### Findings

| ID | CVSS | Vector | Datei:Zeile | Finding | Exploitation | Fix |
| --- | --- | --- | --- | --- | --- | --- |
| SEC-01 | N/A | Supply-chain | `Dockerfile:78` | Production builds pin `golang:1.25.7`; `govulncheck -tags=nogpu ./...` reported the codebase is affected by Go stdlib advisories GO-2026-4601, GO-2026-4602, and GO-2026-4603, all fixed in Go 1.25.8. `Dockerfile.distroless:6` repeats the same pin. | Shipped binaries continue to embed known stdlib vulnerabilities even if app code is otherwise correct. | Bump all builder images and local toolchains to Go 1.25.8 or newer, rebuild images, rerun `govulncheck`. |
| SEC-02 | N/A | CI/CD supply-chain | `.github/workflows/release.yml:19` | Release publishing runs privileged jobs through mutable action tags: `actions/checkout@v6`, `docker/setup-qemu-action@v4`, `docker/setup-buildx-action@v4`, `goreleaser/goreleaser-action@v7`. | A retagged or compromised upstream action can execute arbitrary code in a workflow that has `contents: write` and `packages: write`. | Pin every `uses:` reference to a full commit SHA. |
| SEC-03 | N/A | CI/CD supply-chain | `.github/workflows/api-docs-pages.yml:30` | GitHub Pages deployment also uses mutable action tags: `actions/configure-pages@v5`, `actions/upload-pages-artifact@v4`, `actions/deploy-pages@v4` while holding `pages: write` and `id-token: write`. | A mutable tag compromise lets an attacker ship arbitrary Pages artifacts or abuse OIDC-backed deployment trust. | Pin all Pages actions to full commit SHAs. |
| SEC-04 | N/A | CI/CD policy bypass | `.github/workflows/trivy.yml:63` | Trivy is informational by default. The blocking jobs only run when `workflow_dispatch` sets `enforce=true` or repo variable `TRIVY_ENFORCE=true`; the default path explicitly logs `Enforcement disabled`. | Critical/high OS or library findings can be uploaded to SARIF but still leave PRs and mainline green. | Make enforcement default-on for PRs, main, and release paths; keep non-blocking runs only as an explicit opt-out. |

### Checked And Not Counted As Findings

| Check | Result | Evidence |
| --- | --- | --- |
| Token comparison | OK | `backend/internal/control/auth/token.go:96-105` uses `subtle.ConstantTimeCompare`. |
| Scope fail-open | OK | `backend/internal/control/http/v3/router_v3.go:33-47` panics on missing route policy; `backend/internal/control/authz/policy.go:69-76` returns explicit scope sets. |
| Session cookie flags | OK | `backend/internal/control/http/v3/auth.go:116-141` sets `HttpOnly`, `SameSite=Lax`, `Secure` on HTTPS and rejects non-loopback plain HTTP session exchange. |
| Direct URL SSRF gate | OK | `backend/internal/control/http/v3/handlers_intents.go:67-75` normalizes direct URLs through `backend/internal/platform/net/outbound.go:78-155`. |
| CSRF wildcard handling | OK | `backend/internal/control/middleware/csrf.go:103-117` ignores wildcard trust for unsafe methods; `backend/internal/control/middleware/stack.go:55-60` applies CSRF globally. |

## Phase 2: Architecture And Concurrency

| ID | Severity | Component | Datei:Zeile | Finding | Fix | Effort |
| --- | --- | --- | --- | --- | --- | --- |
| ARC-01 | MEDIUM | Jobs / Picon Pool | `backend/internal/jobs/picon_pool.go:65` | The global picon pool is initialized once, rooted in `context.Background()` (`:111`), starts worker goroutines (`:127-143`), and the audit found no production shutdown path calling `Stop()`. This is a lifecycle hole, not a theoretical one. | Root the pool in the app runtime context, register `Stop()` in shutdown hooks, and remove the singleton hidden start side effect. | M |
| ARC-02 | LOW | Library Service | `backend/internal/library/service.go:29` | `NewService` performs DB writes during construction using `context.Background()` (`:39-44`). Constructor side effects make startup non-cancellable and hide I/O behind object creation. | Move root initialization into an explicit `Start(ctx)` / bootstrap step and keep constructors side-effect free. | S |
| ARC-03 | MEDIUM | OpenWebIF Error Handling | `backend/internal/openwebif/timer_errors.go:11` | Timer error classification still depends on localized substring tokens such as `conflict`, `overlap`, `konflikt`, and `404`. This is the exact string-matching-on-error-text bug class the prompt called out. | Parse stable upstream fields if available; otherwise wrap the status/body once at the transport boundary and stop re-inferring semantics from free text. | M |

## Phase 3: Transcoding Pipeline

| ID | Severity | Symptom | Datei:Zeile | Root Cause | Fix | Test-Strategie |
| --- | --- | --- | --- | --- | --- | --- |
| PIPE-01 | HIGH | Live sessions can keep tuner, FFmpeg, and HLS output alive after the client disappears. | `backend/internal/domain/session/manager/lease_expiry.go:127` | Lease expiry calls `publishStopEvent(...)` but `publishStopEvent` is a stub that only logs `would publish stop event for cleanup` (`:142-150`). That is compounded by `backend/internal/control/http/v3/handlers_hls_serving.go:17-39`, which explicitly excludes lifecycle handling, and `backend/internal/domain/session/manager/heartbeat.go:104-113`, which treats segment writes as activity while FFmpeg is still generating output. | Implement a real stop publication path into the orchestrator, and tie HLS request/context disconnect to session stop or lease revocation. | Start a live session, stop fetching playlist/segments, then assert the FFmpeg PID, lease, and tuner slot are gone after timeout. |
| PIPE-02 | MEDIUM | Repeated VAAPI runtime failures do not demote the hardware path. | `backend/internal/pipeline/hardware/gpu.go:148` | `RecordVAAPIRuntimeFailure()` exists and is tested, but audit found no production caller. `backend/internal/infra/media/ffmpeg/adapter.go:629-668` handles watchdog/process failure without recording a VAAPI runtime failure. | Call the runtime failure recorder on VAAPI-specific process failures and fence profile selection when the threshold trips. | Inject a broken VAAPI runtime, verify the first failure records demotion, and ensure subsequent decisions fall back to non-VAAPI profiles. |
| PIPE-03 | MEDIUM | FFmpeg URL inputs are not protocol constrained beyond the first validated URL. | `backend/internal/infra/media/ffmpeg/plan_builder.go:237` | URL sources are passed straight to FFmpeg with reconnect flags but without `-protocol_whitelist`. The outbound allowlist only validates the initial URL, not nested protocol use inside attacker-controlled manifests. | Add an explicit per-input protocol whitelist, and reject manifests that attempt unsupported child protocols. | Feed a malicious HLS/M3U manifest that references `file:` or `tcp:` children and assert FFmpeg fails closed. |

## Phase 4: Code Quality And Testing

| ID | Severity | File:Line | Finding | Fix |
| --- | --- | --- | --- | --- |
| QLT-01 | MEDIUM | `backend/internal/control/http/v3/intents/service.go:45` | `gocyclo` reports complexity 67 for `(*Service).processStart`. Auth checks, playback decision validation, profile resolution, admission, and source shaping are fused into one routine. | Split into dedicated helpers/services for token validation, profile resolution, source resolution, and admission. |
| QLT-02 | MEDIUM | `backend/internal/infra/media/ffmpeg/plan_builder.go:288` | `planLiveOutput` has complexity 51. Codec selection, VAAPI branching, HLS output assembly, and container behavior are mixed in one function. | Split by output mode and codec family; keep HLS argument assembly separate from codec negotiation. |
| QLT-03 | MEDIUM | `backend/internal/config/runtime_env.go:127` | `readHLSRuntime` has complexity 51. Runtime env parsing and validation are too entangled for reliable change safety. | Replace with a typed runtime config reader that validates field-by-field. |
| QLT-04 | LOW | `backend/internal/config/merge_enigma2_file.go:84` | `enigma2FilePatchFromOpenWebIF` is dead code; `golangci-lint` flags it as unused. | Delete it or wire it into a real call path. |
| QLT-05 | LOW | `backend/internal/control/http/v3/intents/service.go:478` | `pickProfileForCodecsWithCapabilities` is an unused wrapper and currently just forwards to `autocodec`. | Delete the wrapper or route callers through it intentionally. |

### Partial Coverage Signal

The full coverage pass was not reproducible in this environment, but the successful package outputs already show obvious blind spots:

| Package | Observed Coverage |
| --- | --- |
| `github.com/ManuGH/xg2g/internal/app/bootstrap` | `5.1%` |
| `github.com/ManuGH/xg2g/internal/control/http/system` | `19.7%` |
| `github.com/ManuGH/xg2g/cmd/daemon` | `33.0%` |
| `github.com/ManuGH/xg2g/internal/control/authz` | `38.5%` |
| `github.com/ManuGH/xg2g/internal/control/http` | `48.0%` |

### Lint Notes

| Tool | Result | Note |
| --- | --- | --- |
| `golangci-lint` | Confirmed | Real findings were `unused` dead code plus several `gosec` hits. |
| `gosec` SQL findings | Not counted | `backend/cmd/daemon/storage_decision_report_cmd.go:488,587` build SQL from a fixed schema-derived column allowlist, not raw user-controlled fragments. |
| `gosec` file-path findings | Not counted | `backend/cmd/daemon/storage_decision_sweep_cmd.go:404,618,636,715` operate on operator-supplied CLI paths in admin tooling, not remote request input. |

## Phase 5: Summary

### 1. Top Critical/High

| Rang | ID | Severity | Datei:Zeile | One-Liner | Fix-Aufwand |
| --- | --- | --- | --- | --- | --- |
| 1 | PIPE-01 | HIGH | `backend/internal/domain/session/manager/lease_expiry.go:127` | Expired/disconnected live sessions do not actually stop FFmpeg or release tuner resources. | M |
| 2 | SEC-01 | HIGH | `Dockerfile:78` | Production builders pin vulnerable Go 1.25.7 while `govulncheck` reports affected stdlib vulnerabilities fixed in 1.25.8. | S |
| 3 | SEC-02 | HIGH | `.github/workflows/release.yml:19` | Release publishing depends on mutable action tags in a workflow with write-capable privileges. | S |

### 2. Medium

| Rang | ID | Severity | Datei:Zeile | One-Liner | Fix-Aufwand |
| --- | --- | --- | --- | --- | --- |
| 1 | SEC-03 | MEDIUM | `.github/workflows/api-docs-pages.yml:30` | Pages deployment still trusts mutable action tags with deployment privileges. | S |
| 2 | SEC-04 | MEDIUM | `.github/workflows/trivy.yml:63` | Trivy does not block by default, so critical/high findings can ride through CI. | S |
| 3 | ARC-01 | MEDIUM | `backend/internal/jobs/picon_pool.go:65` | Global picon worker pool has no proven production shutdown path. | M |
| 4 | ARC-03 | MEDIUM | `backend/internal/openwebif/timer_errors.go:11` | Timer errors are still derived from localized free text instead of stable typed errors. | M |
| 5 | PIPE-02 | MEDIUM | `backend/internal/pipeline/hardware/gpu.go:148` | VAAPI runtime demotion code exists but is dead in production. | M |
| 6 | PIPE-03 | MEDIUM | `backend/internal/infra/media/ffmpeg/plan_builder.go:237` | FFmpeg URL inputs are not protocol-whitelisted past the initial validated URL. | M |
| 7 | QLT-01 | MEDIUM | `backend/internal/control/http/v3/intents/service.go:45` | `processStart` is still a 67-branch god function. | L |
| 8 | QLT-02 | MEDIUM | `backend/internal/infra/media/ffmpeg/plan_builder.go:288` | `planLiveOutput` remains a 51-branch output builder knot. | L |
| 9 | QLT-03 | MEDIUM | `backend/internal/config/runtime_env.go:127` | Runtime env parsing remains over-complex and brittle. | M |

### 3. Low / Nice-To-Have

| Rang | ID | Datei:Zeile | One-Liner |
| --- | --- | --- | --- |
| 1 | ARC-02 | `backend/internal/library/service.go:29` | Constructor still performs DB writes on `context.Background()`. |
| 2 | QLT-04 | `backend/internal/config/merge_enigma2_file.go:84` | Dead helper left behind. |
| 3 | QLT-05 | `backend/internal/control/http/v3/intents/service.go:478` | Dead wrapper left behind. |

### 4. Known Issue Status

| ID | Status | Evidence | Note |
| --- | --- | --- | --- |
| CON-03 | Fixed | `backend/internal/pipeline/bus/memory_bus.go:52-67` | No `default:` backpressure drop path remains; publish only aborts on `ctx.Done()`. |
| ERR-03 | Fixed | `backend/internal/app/bootstrap/bootstrap.go:105-110` | Resume store init failure is logged and falls back to memory instead of being swallowed silently. |
| CON-01 / CON-02 | Fixed | `backend/internal/control/vod/prober.go:43-66`, `backend/internal/control/vod/manager.go:83-92`, `backend/internal/control/vod/manager.go:661-665` | Prober workers and build monitors are rooted in runtime context and joined on shutdown. |
| CFG-01 | Partially fixed | `backend/cmd/daemon/main.go` has no direct `os.Getenv` hits; remaining env reads are centralized in `backend/internal/config/*.go` | The specific `main.go` leakage is gone, but env access still exists by design in the config layer. |
| DG-02 | Open | `go list` import fan-out observed `internal/control/http/v3 = 85` | Still a god-package. Not fixed. |
| ERR-01 | Open | `backend/internal/openwebif/timer_errors.go:11-31,69-79` | String-matching on error text is still present. |
| CFG-04 | Fixed | `backend/internal/control/http/v3/server.go:369-373` | Library store is now closed during shutdown. |
| ERR-05 | Fixed | `backend/internal/control/authz/policy.go:78-86` | `MustScopes` no longer panics on unknown operations. |

### 5. Sprint Plan

- Week 1: Security. Bump Go toolchains, pin mutable GitHub Actions, and make Trivy blocking by default.
- Week 2: Stability. Implement real lease-expiry stop cleanup, then fix the picon pool lifecycle and VAAPI runtime demotion path.
- Week 3: Error and Config Cleanup. Remove localized string-matching for timer errors and split runtime env parsing into typed validators.
- Week 4: Structure and Tests. Break up `v3` intent/start logic and FFmpeg output planning, then raise coverage in `internal/app/bootstrap` and `internal/control/http/system`.

### 6. Validation Commands

```bash
cd /root/xg2g/backend && go test -race ./...
cd /root/xg2g/backend && /root/go/bin/govulncheck -tags=nogpu ./...
cd /root/xg2g/backend && /root/go/bin/golangci-lint run ./...
cd /root/xg2g && rg -n "context.Background\\(\\)" backend/internal --glob '*.go' | grep -v _test.go | wc -l
cd /root/xg2g && rg -n "os.Getenv|os.LookupEnv" backend/internal backend/cmd --glob '*.go' | grep -v _test.go | wc -l
cd /root/xg2g && rg -n '"broken pipe"|"connection reset"|"Konflikt"|"Conflict"|"overlap"|"not found"' backend/internal --glob '*.go' | wc -l
```
