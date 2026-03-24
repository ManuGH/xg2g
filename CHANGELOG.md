# Changelog

## Unreleased

### Release Notes

- Playback decisions now follow an intent-based profile strategy with a quality ladder for audio and video.
- The Web player now derives client capabilities via runtime browser probing instead of relying only on user-agent heuristics.
- Operators can now override playback behavior per source, including forced intents, maximum quality rungs, and disabled client fallbacks.
- The system now reacts to host pressure and can automatically downgrade profiles under load to keep playback serviceable.
- Playback startup failures are now classified through structured input preflight diagnostics with clearer fallback behavior.
- Playback telemetry now exposes quality rungs, host overrides, operator rules, target profiles, and preflight reasons across API and player traces.

### Technical Notes

- Decision, session trace, OpenAPI, and WebUI contracts were extended with a consistent playback observability model.
- Profile resolution, canonicalization, and hash stability were expanded to support variant-aware caching and deterministic target profiles.
- Client matrix fixtures, table-driven tests, and hysteresis-based host pressure handling now cover the new playback decision paths.
- Configuration surfaces and ops documentation were extended for playback operator rules and the new diagnostic fields.

### Bug Fixes

- Lock Duration preservation on probe failure
- Resolve buffering, dropped frames, and UI autohide issues
- Robust digest extraction for stability gates
- Restore valid action pins (fix invalid SHAs for setup-go, setup-node, artifact actions)
- Align Dockerfile.distroless Go version with go.mod (1.25.6)
- Restore valid action pins for all workflows and add verification gate
- Allow GOTOOLCHAIN=auto in CI to satisfy setup-go and toolchain policy guard
- Fix embed pattern and add cache-dependency-path for setup-go
- Ensure WebUI dist directory exists for Go embedding even if build is skipped
- Satisfy TestUIEmbedContract by creating dummy index.html in dist
- Use temp dir for XG2G_STORE_PATH in verification tests to avoid permission denied
- Use temp dir for XG2G_STORE_PATH in config loader tests
- Use temp dir for XG2G_STORE_PATH in all loader tests
- Use temp dir for XG2G_STORE_PATH in all tests to fix CI permission errors
- Update config surfaces and fix shellcheck warning in ci.yml
- Resolve persisting CI permission errors and config audit mismatches
- Resolve flaky recording cache eviction race
- Install ffmpeg for system tests
- Inject XG2G_STORE_PATH to bypass permission errors in CI
- Close SSRF gap in preflight validation
- Honor timer edit capability (#165)
- Dvr edit fail-closed and audio-only mapping (#166)
- Add required APITokenScopes to smoke test environment (#179)
- Restore auth/vod/ffmpeg contracts (stability) (#190)
- Engine-first negotiation; adapter plans from decision output (#202)
- Complete lifecycle join + shutdown resource closure (#212)
- Preserve error chains and remove string-matching paths (#222)
- Clean up process lifecycle and health checks (#223)
- Always register runtime store close hooks (#225)
- Align default segment length with config registry (#224)
- Centralize HLS segment default across registry and ffmpeg (#231)
- Harden stream URL host handling and capability cache expiry (#228)
- Fail fast when channel/series manager state cannot load (#229)
- Validate XG2G_MAX_UNTRACKED in pre-push sanity hook (#232)
- Avoid bootstrap logger reconfigure race (#234)
- Satisfy env-access + lint + gosec
- Align player with generated client contract
- Restore v3 player contract gates
- Update v3 fanout baseline to 79
- Include playback_mode in intent params
- Remove hls frag duration access for gate K

### Documentation

- Walkthrough for drift simulation
- Walkthrough for drift simulation
- Sync config surfaces documentation
- Add CI failure playbook and external audit mode
- Link CI playbook and audit mode from CI policy
- Regenerate config surfaces
- Update CONFIG_SURFACES
- Regenerate CONFIG_SURFACES
- Render versioned artifacts from templates
- Regenerate CONFIG_SURFACES for embedded index bundle

### Features

- Add 'status' and 'report' commands and /status API
- Harden status api and cli (phase 7.1)
- Add homelab profile (env flags + edge logging + metrics)
- Implement Best Practice 2026 architecture
- Finalize timer update refactoring with selective fallback and prometheus metrics
- VAAPI GPU transcoding via ProfileSpec (#181)
- Client codec negotiation + best-profile selection (#184)
- Sync baseline to b6c40d8 (#221)
- Duration truth, thin-client governance, safari audit, and contract hardening (#252)
- Restore blocked-preparing semantics + harden probe orchestration boundary (#255)

### Miscellaneous

- Repository hygiene and CI/CD stabilization (fix embed drift)
- Consolidate CI workflows and prune repository metadata
- Upgrade to Node 22 and Debian Trixie (2026 stable) with fixed build
- Final seal checks and config registry fix
- Sync remaining unmerged changes (vendor, webui, tests, docs)
- Regenerate config surfaces to match new registry defaults
- Sync docker-compose.yml with latest templates
- Harden CI quality-gates with prioritized zero-drift checks
- Standardize TOOLCHAIN_ENV and upgrade Go to 1.25.6 for security
- Baseline CI hardening (Chef-Review implementation)
- Stabilize main (sync config surfaces and tidy go.mod)
- Pin CI badge to main branch
- Debug ffmpeg path
- Increase timeout to 30m
- Regenerate config surfaces
- Smoke required-check pipeline after branch-protection update (#192)
- Bump the actions group across 1 directory with 3 updates (#191)
- Bump the npm-deps group across 1 directory with 11 updates (#203)
- Bump the npm-deps group in /webui with 4 updates (#237)
- Bump ajv from 8.17.1 to 8.18.0 in /e2e/fixture-server (#233)
- Reduce merge blockers with fail-fast and metadata repair (#242)
- Fixes for ffmpeg logging, url validation, and v3player formatting
- Consolidate minor fixes and updates
- Bump the actions group with 7 updates + fix download-artifact comment
- Bump minimatch in /webui (#253)
- Bump rollup from 4.57.1 to 4.59.0 in /webui (#254)
- Regenerate hard-mode artifacts
- Bump VERSION to v3.1.8 and render docs
- Refresh embedded dist and config surfaces
- Major restructuring and release automation (Phases 1-3)

### Refactor

- Stop importing internal/core/profile
- Enforce single runtime wiring ownership (#213)
- Split v3 router registration by bounded context (#215)
- Inject constructor deps from composition root (#216)
- Inject v3 orchestrator via factory port (#217)
- Split generated server interfaces by domain tags (#218)
- Narrow runtime ports and enforce module boundaries (#219)
- Typed config parse errors and stream URL strategies (#220)
- Move HLS content types to platform httpx (#226)
- Enforce module dependency boundaries in intent flow (#230)
- Phase1 architecture runtime fixes (#238)

### Testing

- Add mechanical contract and redaction gates (phase 7.1)
- Add SSRF bypass coverage for IPv6 and normalization
- Prevent 410 Gone retry loop in V3Player (#199)
- Retry on 503 during session readiness (#200)
- Fix validate tests paths for new monorepo layout

### Breaking

- Sprint 4 step 1-11 - remove legacy HTTP surface (#210)

### Build

- Bump the actions group across 1 directory with 4 updates (#148)
- Bump fastify from 5.7.1 to 5.7.3 in /e2e/fixture-server (#153)
- Bump the npm-deps group across 1 directory with 2 updates (#155)
- Bump i18next in /webui in the npm-deps group
- Bump the actions group with 4 updates
- Bump the actions group across 1 directory with 2 updates
- Bump the npm-deps group across 1 directory with 4 updates

### Ci

- Split concurrency + pin tooling versions
- Split pr gate and nightly workflows
- Stabilize runs (repo-wide concurrency + smoke) and fix release scan steps
- Build webui only when affected (node-optional pr gate)
- Enforce cto pr gate policy
- Run required gates on push(main) + document triggers
- Fix Scorecard SARIF upload + runner-based gosec
- Avoid cross-workflow cancellations
- Queue runs instead of canceling
- Add scheduled deep suite (race, integration, security, governance) with artifacts and UTC schedule (#193)
- Add deep-suite prereqs for race signal (#194)
- Add deep-suite failure issue alerting (#195)
- Harden WebUI gates and decouple nightly lint (#236)

### Codec

- Engine-driven negotiation and adapter planning (#201)

### Config

- Remove overdue deprecation docs; warn on removed env vars

### Dev

- Make golangci-lint hook deterministic (go1.25 compatible) (#246)

### Governance

- Codify solo maintainer merge policy (#197)
- Fail closed on empty PR delta (#204)

### Hardening

- Sprint 2/3 completion with lifecycle, timer errors, and config foundation (#209)

### Metrics

- Add decision.summary counters (#207)

### Obs

- Add decision summary fields and structured planner log (#205)

### Ops

- Add workspace cleanup script and fail-closed hygiene (#198)
- Add pre-push sanity guard for shared worktrees (#208)

### Phase8

- Wire-up verification worker, status api and cli drift display

### Sec

- Pin GitHub Actions to SHAs and upgrade Go to 1.25.3
- Complete security hardening and action pinning
- Fix codeql sub-action names in workflows
- Finalize security hardening (pin actions and fix syntax)

### Security

- Redact credentials from receiver URLs in logs (#183)
- Live pipeline hardening — fail-closed JWT, CORS allowlist, race fixes

### V3

- Validate live serviceRef, align RFC7807 /problems types, fix recordings status fixtures, add admission fail-closed contract

### Verification

- Worker hardening + store implementation + checks interface

### Webui

- Migrate styles to CSS modules (#180)
- Handle 410 Gone in session readiness (#196)
- Use canonical playback_decision_token for live intents (#245)

### Bug Fixes

- Improve stream startup responsiveness
- Correct HLS playlist sizing logic for low latency
- Comment out enigma2 section to satisfy schema validation
- Harden hermetic proof against false positives and CI failures
- Align dist asset contract with embed path
- Resolve WebUI path regressions in Docker and workflows
- Restore stability by resolving validation regression and security false positives
- Standardize security scan and stabilize HLS smoke tests
- Resolve registry/loader mismatch for CI stability
- Implement fail-closed state canonicalization for streams
- Complete contract governance - streams + vod playback info
- Configgen emit git add cmd/configgen/main.go config.generated.example.yaml docs/guides/CONFIGURATION.md docs/guides/config.schema.json docs/guides/CONFIG_SURFACES.md fixtures/capabilities/ fixtures/GOVERNANCE_CAPABILITIES_BASELINE.json 2>&1 && git status --shortfloat for float64 + regenerate fixtures
- Enforce ADR path case sensitivity (docs/ADR/ not docs/adr/)
- Harden verify-doc-links.sh (fail-closed, index-aware)
- Restore ADR integrity (ADR-003 placeholder, ADR-009 link)
- Correct INV-NORM-003 naming and documentation
- Close ADR-021 bypass in resume and capabilities stores
- Remove hardcoded bolt backend from daemon main
- Document at-most-once lease behavior in orchestrator
- Use correct context for heartbeat renewal
- Cleanup v2 unit tests (env isolation, proxy mock)
- Resolve ci lint failures (errcheck, gosec, staticcheck, unused)

### Documentation

- Complete Phase 5 release and hardening walkthrough
- Formalize security trust boundaries and harden HLS smoke tests
- Use relative links for GitHub compatibility
- Add Enigma2 streaming topology
- Sanitize PREFLIGHT examples
- Neutralize stream processing terminology
- Complete OSCam/decryption terminology removal
- Remove obsolete documentation
- Remove duplicate and obsolete documentation
- Aggressive consolidation - final cleanup
- Add operator-grade configuration guide (registry-aligned defaults)
- Add ADR for Playback Decision Engine (PR-P4-1)
- Add v3 contract purity ribbon-cut checklist
- Fix broken references in ADR-002 and ADR-004
- Fix broken relative link to ARCHITECTURE.md
- Remove obsolete backup files (DESIGN.md backups)
- Add decision ownership gate to invariants
- Phase 2 - Contract & OpenAPI Updates
- Add known risks and 2026 requirements to vision
- Sync config surfaces after merges
- Sync config surfaces and fix UI purity false positive
- Update governance baseline manifest for consolidated main
- Sync config surfaces
- Final config surface sync

### Features

- Harden preflight logic and transport defaults
- Optimize FFmpeg flags for corrupted DVB satellite streams
- Add configurable preflight timeout and enhanced observability
- Standardise high-quality streaming and 10s preflight timeout
- Upgrade to Gold Standard video quality (CRF 18)
- Prove hermetic build guarantees under adversarial conditions
- Complete VODConfig typed migration
- Add fail-closed VOD conflict detection (YAML+ENV)
- Extend OpenAPI with P4-1 playback decision schemas
- Add P4-1 contract matrix with 8 golden snapshots
- Add P4-1 contract matrix with 8 golden snapshots
- PR-3 Lint Sweep, Governance Gates, and Bugfixes
- Standardize v3 openapi and enforce contract governance hardrails
- Finalize Phase 3 Proof System (CTO-Grade)
- Core architecture foundation (docs/model/store)
- Add worker and api implementation aligned with core
- Institutionalized governance system (Zero-Drift, Runtime Truth, Continuous Verification)

### Miscellaneous

- Finalize v3 contract governance and CI hardening
- Fix linter warnings across scripts and docs
- Consolidate local development state on main
- Sync vendor directory after merges
- Sync vendor directory
- Baseline for release test
- Final logic baseline

### Performance

- Reduce HLS segment size to 2s for instant start
- Enable Low-Latency mode (ultrafast + CRF 20)
- Reduce HLS segment size to 1 second for instant startup

### Refactor

- Use tagged switch for source type dispatch
- Use generic channel names throughout
- Remove 'Premium' brand terminology

### Testing

- Use generic channel names in slugify tests
- Centralize media mocks and cover volume restore
- Centralize i18n stub in setup
- Add unit tests for decode.go helpers
- Add property tests for normalize.go
- Add factory tests verifying ADR-021 enforcement
- Add SQLite template tests (Option C)
- Add phase 6b integration flow tests

### Adr

- Enforce BoltDB/BadgerDB sunset (ADR-021, PR-1)

### Build

- Downgrade Go version to 1.24.11 for CI compatibility
- Update Dockerfile.distroless to Go 1.24.11
- Integrate storage purity gate into verify-purity
- Bump lodash from 4.17.21 to 4.17.23 in /webui
- Bump the actions group across 1 directory with 6 updates

### Ci

- Add contract-matrix gate (PR-P4-1)
- Fix shellcheck warnings in workflows (SC2086, SC2046, SC2155)

### Config

- Make metrics listen opt-in across registry and docs
- Regenerate config surfaces from registry
- Update inventory with self-referential files

### Governance

- Fix decision ownership gate allowlisting invariants + contract fixtures
- Harden decision ownership import check (fixed-string grep)
- Fix openapi lint error count parsing
- Remove legacy badger store (ADR-021)
- Allow intentional schema validation panics

### Refine

- Final merge-readiness hardening

### V3

- Add skeleton (fsm/store/bus/api)

### Webui

- Fix start path stale-state, improve regenerate recovery, add serviceRef start tests

### Wip

- Add VODConfig struct (partial - follow-up required)
- Phase 4 checkpoint - ADR migration + SQLite + decision decode
- Add migration tests and cleanup temp files

### Bug Fixes

- Switch healthcheck to liveness probe (-mode live)
- Correct misleading error message in GetEpg
- Preserve cache headers and stabilize asset test
- Gate client_ip output
- Consolidate OpenAPI codegen to Makefile (v2 + v3)
- Wire v3 recordings resolver to prevent nil pointer panic
- Resolve 3 CRITICAL build-blocking compile errors
- Update bootstrap test mocks for domain resolver signature
- Correct MetadataManager interface signatures
- Restore CI integrity and API consistency
- RFC7807 compliance in ProbeRecordingMp4
- Bootstrap test failures - contract drift and async handling
- Test wiring - mock matches decoded recordingID
- Replace fake SHA pins with valid action tags in quality-gates.yml
- Replace all SHA pins with stable tag references
- Allow golangci-lint installation with GOFLAGS override
- Install golangci-lint v2 (config compatibility)
- Use specific golangci-lint version v2.8.0
- Use go install for golangci-lint v2.8.0 (pragmatic solution)
- Golangci-lint v2.8.0 installation (correct tabs)
- Use golangci-lint@latest (v2.8.0 tag path issue)
- Address remaining blockers for full Green status

### Documentation

- Finalize v3.1.5 system operations hardening and runbooks
- Consolidate review artifacts

### Features

- Proof Pack for Live/DVR/VOD separation & test hardening
- Major refactoring of API, middleware, config, and VOD subsystems
- Strict PlaybackInfo Contract Hardening
- PIDE Implementation, EPG Fixes, and Contract Gates
- Goldstandard test fix - production-grade RecordingsService mock
- Hermetic builds with go run -mod=vendor (offline ZIP support)
- Hermetic builds with go run -mod=vendor (offline ZIP support)
- Add oapi-codegen dependencies for hermetic builds
- Implement Go 1.24 tool directive (2026 best practice)

### Miscellaneous

- Enforce layering rules - achieve 10/10 architecture
- Repository hygiene - binaries, docs, ADRs, scripts
- Update dependencies
- Fix workflow versions and add webui lint
- Upgrade actions to latest major versions
- Upgrade checkout to v6 (absolute latest)
- Resolve lint debt and consolidate artifacts
- Enforce strict review-zip purity
- Audit and fix gosec/errcheck issues
- Strict errcheck and adapter nosec justification
- Finalize docs hardening and architecture gates
- Finalize CI hardening and governance gates (all GREEN)

### P0

- Ban URL tokens and enforce session-only media

### Refactor

- Remove legacy VOD resolver hooks
- Cleanup dead code and consolidate recordings domain
- 2026 best practices - clean architecture

### Testing

- Align result=false contract
- Stabilize stampede cleanup
- Skip without ffmpeg
- Align about mock shape
- Align runtime playlist filename

### Governance

- CTO mandate - critical fixes (zero tolerance)
- Acceptance hardening - SHA pinning + hygiene gate
- Go 1.25.5 security upgrade + coverage policy hardening

### Release

- V3.1.5 - hardened no-tools runtime stabilization

### Release

- V3.1.4 - hardened RC2 with mechanical proofs

### Miscellaneous

- Bump version to 3.1.2

### Features

- Harden V3 API and Probe (RFC7807, Padding, Auth)

### Miscellaneous

- Set default version to 'dev' for dynamic versioning
- Bump the actions group across 1 directory with 5 updates (#129)

### Performance

- Optimize Dockerfile for faster builds

### Features

- Phase 6 Step 1 - ProblemDetails standardization
- Complete Rust removal - Pure Go v2.0.0

### Miscellaneous

- Bump version to v3.1.0

### Bug Fixes

- Unify receiver base URL handling and fix session state coverage
- Add missing Service schema definition
- Regenerate V3 spec and add CI drift-check
- Remove instantTune from V3 spec
- Build errors after OpenAPI regeneration
- Critical CI gate fixes - fail-closed enforcement (PR#6)
- Add ripgrep dependency check per CTO review

### Documentation

- Add health and readiness endpoint documentation
- WebUI Thin Client Audit - 2 violations found
- Add ADR-014 app structure & ownership boundaries

### Features

- Add LAN guard middleware and comprehensive auth enforcement tests
- Implement Phase 2 idempotency and lease centralization (ADR-003)
- Implement progressive VOD recording system with adaptive retry
- Comprehensive VOD system with multi-strategy playback and resilience
- Unified LIVE-DVR architecture with Safari MediaError 4 fix
- Comprehensive v3 system improvements and production hardening
- PR 4 - Config Cleanup & Security Hardening
- Add streaming profile configuration
- Add streaming config to V3 API
- Add streaming config to V3 API
- Fix thin client violations (PR 5.1)
- Contract Compliance Phase + Phase-1 Setup
- Backend Phase 1 - Session Lease Semantics
- Phase 3 - Frontend Heartbeat Integration (FRONTEND-ONLY)
- Complete session domain migration (step 3a/3b)
- Implement CTO binary gates - bounded concurrency, eliminate magic defaults, platform abstraction
- Implement CTO binary gates - bounded concurrency, eliminate magic defaults, platform abstraction
- Phase 1 PR #1 - Foundation skeleton + Gate A
- Phase 1 PR #1 - Foundation skeleton + Gate A enforcement
- Complete Slice 5.3 & 5.4 (GetStreams Hardening)

### Miscellaneous

- Remove dead code and add cleanup TODOs
- Migrate test assets to testdata/ structure
- Complete repository hygiene (PR#4)
- Professional repo cleanup and CI alignment
- Migrate v3 read-only endpoints (Slice 5.1)

### Refactor

- Improve config management and enhance auth test coverage
- Complete ADR-014 Phase 0
- PR #2 Slice 2.1 - move internal/auth to internal/control/auth
- PR #2 Slice 2.2 - move internal/api/middleware to internal/control/middleware
- PR #3 Slice 3.1 - extract secure file server to control/http
- PR #3 Slice 3.2 - move UI embed + serving to control/http

### Ci

- Add large file prevention and repo health checks
- Add complexity gates and quality enforcement (PR#5)

### Documentation

- Document startup lease flush invariant (v3.0.3)

### Bug Fixes

- Backend lease release on terminal states (v3.0.2)

### Bug Fixes

- Align workflows with strategy-a path standards (fix embed failures)
- Harden workflow paths and build triggers (full coverage)
- Harden triggers (api/**) and build guards (strict checks)
- Add webui integrity checks (hash/count) and vite assumption comments
- Finalize ci script hygiene and update governance docs with Vite assumption
- Use -tags=nogpu for analysis workflows (codeql, gosec, govulncheck)
- Align pathing (dist) and tags (nogpu) with Strategy A
- Repair triggers, race tests (nogpu), artifact path, and summary gate
- Refine Unraid CA template (remove redundant config, improve CA compliance)
- Hardcode 8088 in Unraid WebUI URL for host mode safety
- Enforce port 8088 via XG2G_LISTEN in Unraid template to match WebUI link
- Improve test reliability and recovery logic
- Add searchEvents container for semantic correctness
- Sync OpenAPI client and fix config validation
- Restrict anonymous users to read-only access
- Update smoke test to use v3 API
- Resolve errcheck and gosec warnings
- Resolve staticcheck warnings in recordings.go
- Correct datetime-local formatting and close EPG i18n gaps
- Extract path from Enigma2 service reference for local playback

### Documentation

- Add release governance framework (test matrix, checklist, goreleaser footer)
- Add CI architecture matrix (Gate vs Nightly) to TESTING.md
- Add INSTALL_NAS.md for Unraid/Synology users
- Clarify timestamp units and add DEFAULT_TIME_RANGE
- Finalize v3.0.0 release notes and public README
- Add v3.1.0 release notes and Enigma2 ServiceRef documentation
- Restructure architecture documentation and add new specs

### Features

- Add Unraid CA template (xg2g.xml)
- Use bash in Unraid template for improved console experience
- Implement profile resolution with user-agent detection
- Enhance profile system with configurable encoding parameters
- Add comprehensive Enigma2 client configuration and resilience
- Make bouquet configuration optional and support all bouquets
- Add setup wizard with connection validation and simplified docker config
- Add monitoring stack and fix integration test
- Add dynamic discovery of recording locations
- Add TypeScript support with incremental migration strategy
- Migrate V3Player to TypeScript with full type safety
- Implement Context API for centralized state management
- Migrate Logs to TypeScript with direct client-ts SDK (Phase 4.2)
- Complete Phase 4 TypeScript migration with guardrails
- Add EPG feature types (Phase 5 Step 1)
- Implement data layer with zero legacy imports (Phase 5 Step 2)
- Implement state machine reducer (Phase 5 Step 3)
- Extract UI components with typed props (Phase 5 Step 4)
- Cutover to new feature architecture (Phase 5 Step 5)
- Remove compatibility layer and complete TypeScript migration (Phase 5 Step 6)
- Migrate remaining components to TypeScript (Phase 3)
- Add advanced UX features and stats overlay (v3.0.0)
- Comprehensive 2026-ready security hardening
- 2026-ready v3 API hardening
- Production-ready admission control and observability hardening
- Harden recordings API with POSIX path handling and security improvements
- Comprehensive recording playback and HLS hardening
- Add PUT /series-rules/{id} for proper rule updates
- Local-first recording playback with stability gate
- Add ffprobe duration detection for local recordings
- Add VOD seeking and local playback optimizations
- Add VOD scrubber and recording enhancements
- Add bulk delete for recordings and VOD build status

### Miscellaneous

- Convert to nightly/extended workflow (remove PR triggers, redundant setup)
- Remove unused Rust transcoder code
- Modernize Dockerfile with latest versions and optimizations
- Bump the actions group with 14 updates (#116)

### Performance

- Implement lazy loading and chunk splitting (Phase 4.1)
- Lazy load Player component (Phase 6A)

### Refactor

- Migrate API from v2 to v3 and modernize streaming architecture
- Streamline Enigma2 client initialization and telemetry
- Fix version to v3, remove legacy version handling
- Migrate recording endpoints to generated router
- Code quality improvements

### Styling

- Apply optional refinements to Unraid XML (clean metadata, remove port config)

### Testing

- Fix engine_test for updated AddRule signature
- Add UpdateSeriesRule API tests (200/404/400)

### V3

- Enforce recording_id bounds + align OpenAPI/SDK

### Webui

- Add i18n language switch + harden V3Player auth/session flow
- Disable strict mode to prevent V3Player race conditions

### Bug Fixes

- Prevent premature finalization of sessions that never started
- Harden ShadowClient and BadgerStore against critical hazards
- Production hardening - Phase 10-12 integration and critical fixes
- Correct embedded UI path to use 'dist' directory
- Enable muted autoplay for V3 player
- Enable V3 by default for proper setup experience
- Add V3 settings to .env.example, remove obsolete proxy options
- Correct V3_WORKER_ENABLED default in v3-setup guide
- Replace CodeQL autobuild with explicit build steps
- Standardize codeql actions to v3 to resolve version mismatch
- Resolve gosec findings and fix transcoder test assertions
- Resolve all golangci-lint issues (errcheck, gosec, staticcheck)
- Correct WebUI path in release and fix Dockerfile Node version
- Update release workflow to prevent dirty git state and update webui assets
- Remove redundant webui build in release workflow to ensure clean git state
- Complete CI-first migration (update gitignore and release workflow)
- Update stickier go:embed directive and document binary capabilities
- Polish release engineering (robust embed, stricter CI check, correct docs)

### Documentation

- Streaming routing notes and ADRs
- Update CHANGELOG, README, and add INTERNALS documentation
- Add V3 setup and deployment guide
- Update documentation to reflect V3 as production-ready
- Cleanup V2-specific docs, update TROUBLESHOOTING for V3
- Add DEVELOPMENT.md guide for simplified build workflow
- Add Documentation section with key guide links
- Clarify V3 as production streaming architecture
- Improve Quick Start clarity for AI setup
- Clarify separate frontend/backend development workflow
- Improve consistency across setup guides
- Add minimal .env template for simplified Quick Start
- Remove legacy 'proxy' terminology, use 'V3 streaming'

### Features

- Canary critical fixes (preflight, observability, hardening)
- Shadow canary (traffic guard, async client, metrics)
- Implement PR 9-3 cleanup and sweeper
- Shadow canary (traffic guard, async client, metrics) (#111)
- Phase 9-4 metrics refactoring and session lifecycle improvements
- Implement V3 HLS streaming backend with refined buffering and resilience
- Add V3 player component and update build assets
- Auto-inherit XG2G_V3_E2_HOST from XG2G_OWI_BASE
- Release v3.0.0 framework - remove query auth, strict config, add versioning docs

### Miscellaneous

- Remove unused hls helper code
- Syncing all remaining V3 changes and fixes
- Cleanup legacy scripts, docs, and testdata
- Change default port from 8080 to 8088
- Add markdownlint config to allow duplicate headings in changelog
- Migrate to CI-first release strategy (remove dist from git, restore CI build)
- Refine CI strategy (robust copy, sanity checks, strict ignores)
- Finalize CI strategy (expose .github, simplify runtime deps)

### Refactor

- Unify circuit breaker and cleanup technical debt
- Remove legacy proxy package and migrate to V3 architecture

### Testing

- Lock capability routing
- Assert routing precedence
- Add verification for strict mode breaking change

### Api

- Warm up HLS during stream preflight

### Build

- Update embedded WebUI assets

### Proxy

- Capability-based routing and readiness
- Add bounded routing telemetry
- Normalize routing default reason

### V3

- Core Architecture & Docs (#110)
- Worker & API Implementation (#109)
<!-- generated by git-cliff -->
### Behavioral Changes (v3.4.2)
Operational changes:
- `verify-compose-contract.sh` now validates required `env_file` and `/var/lib/xg2g` mounts against the source compose files instead of the normalized `docker compose config` output, preventing false fail-closed restarts on live hosts.
- The systemd + Compose runbook now records the March 24, 2026 live-host installation drift so operators can distinguish target-state docs from currently deployed runtime truth during upgrades.
- `release-prepare.sh` now updates `DIGESTS.lock`, `RELEASE_MANIFEST.json`, and the fallback version string structurally from the JSON source of truth, defaults to `GOTOOLCHAIN=auto`, and uses a writable explicit `GOCACHE`, so tagged release preparation no longer aborts on stale local toolchains or unwritable host cache defaults before manifest generation.


### Behavioral Changes (v3.4.3)



