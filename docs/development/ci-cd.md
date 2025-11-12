# CI/CD Pipeline Documentation

This document describes the comprehensive CI/CD setup for xg2g, including automated testing, linting, security scanning, and deployment workflows.

## Overview

xg2g uses GitHub Actions for CI/CD with multiple specialized workflows:

| Workflow | Purpose | Trigger | Badge |
|----------|---------|---------|-------|
| **ci.yml** | Standard CI with tests | Push/PR to main | [![CI](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml/badge.svg)](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml) |
| **docker-integration-tests.yml** | Full Rust FFI + GPU tests | Push/PR to main | [![Docker Integration Tests](https://github.com/ManuGH/xg2g/actions/workflows/docker-integration-tests.yml/badge.svg)](https://github.com/ManuGH/xg2g/actions/workflows/docker-integration-tests.yml) |
| **hardcore-ci.yml** | Comprehensive quality checks | Push/PR to main | [![Hardcore CI](https://github.com/ManuGH/xg2g/actions/workflows/hardcore-ci.yml/badge.svg)](https://github.com/ManuGH/xg2g/actions/workflows/hardcore-ci.yml) |
| **codeql.yml** | Security analysis | Push/PR/Schedule | [![CodeQL](https://github.com/ManuGH/xg2g/actions/workflows/codeql.yml/badge.svg)](https://github.com/ManuGH/xg2g/actions/workflows/codeql.yml) |
| **gosec.yml** | Go security scanner | Push/PR | ![gosec](https://github.com/ManuGH/xg2g/actions/workflows/gosec.yml/badge.svg) |
| **govulncheck.yml** | Vulnerability scanning | Push/PR/Schedule | ![govulncheck](https://github.com/ManuGH/xg2g/actions/workflows/govulncheck.yml/badge.svg) |
| **container-security.yml** | Container vulnerability scanning | Push/PR/Schedule | [![Container Security](https://github.com/ManuGH/xg2g/actions/workflows/container-security.yml/badge.svg)](https://github.com/ManuGH/xg2g/actions/workflows/container-security.yml) |
| **sbom.yml** | SBOM generation | Push/PR/Schedule | [![SBOM](https://github.com/ManuGH/xg2g/actions/workflows/sbom.yml/badge.svg)](https://github.com/ManuGH/xg2g/actions/workflows/sbom.yml) |
| **docker.yml** | Docker build & push | Push/PR/Tag | [![Docker](https://github.com/ManuGH/xg2g/actions/workflows/docker.yml/badge.svg)](https://github.com/ManuGH/xg2g/actions/workflows/docker.yml) |
| **release.yml** | Create GitHub releases | Tag push | - |
| **pr-conventional-commits.yml** | Enforce commit conventions | PR | - |

## Workflow Details

### 1. Standard CI (`ci.yml`)

**Purpose**: Fast feedback on code changes (nogpu builds)

### 2. Docker Integration Tests (`docker-integration-tests.yml`)

**Purpose**: Full production validation with Rust FFI + GPU support

**CRITICAL**: These tests verify the actual production build including Rust FFI, which is NOT tested in standard CI.

**Steps**:
1. **Build Docker image** to `go-builder` stage (includes all dependencies)
2. **Run tests inside Docker** with `-tags=gpu` (full Rust FFI)
3. **Verify Rust library** is present and linked
4. **Build final production image** (verification)
5. **Run smoke test** of production binary

**Why This Matters**:
- Standard CI uses `-tags=nogpu` (stub implementations, fast)
- Docker tests use `-tags=gpu` (real Rust FFI, slower but comprehensive)
- This is the ONLY place Rust FFI integration is actually tested before deployment

**Environment**:
```yaml
- Rust library: libxg2g_transcoder.so
- CGO: Enabled
- Build tags: gpu
- Test all packages including internal/transcoder
```

**Test Output**:
```
✅ Rust library built: transcoder/target/release/libxg2g_transcoder.so
✅ All tests passed with Rust FFI
✅ Production build verified
```

### 3. Standard CI (continued)

**Steps**:
1. **Module Verification**: `go mod tidy && go mod download && go mod verify`
2. **Build**: Compile daemon with version injection
3. **Unit Tests**: `go test -race -coverprofile=coverage.out`
4. **OpenAPI Linting**: Validate API spec with Redocly
5. **Integration Tests**:
   - Start OpenWebIF stub server
   - Run xg2g daemon
   - Test /api/refresh and /api/status endpoints
   - Validate M3U and XMLTV output
6. **golangci-lint**: Comprehensive code quality checks
7. **Markdown Lint**: Documentation quality checks
8. **Artifacts Upload**: Logs, coverage, generated files

**Key Features**:
- **Coverage Threshold**: Enforced minimum coverage
- **Concurrency**: Cancel in-progress runs
- **Daily Schedule**: Runs daily at 02:17 UTC
- **Job Summary**: GitHub step summary with metrics

### 2. Hardcore CI (`hardcore-ci.yml`)

**Purpose**: Enterprise-grade quality assurance

**Jobs**:

#### Static Analysis & Security
- Go module cache optimization
- golangci-lint with 30+ linters enabled
- gosec security scanning
- Trivy filesystem vulnerability scanning
- License compliance checking
- Dependency review

#### Build & Test Matrix
- **Multi-OS**: Linux, macOS, Windows
- **Go Versions**: 1.25.0 (configurable)
- **Race Detection**: `go test -race`
- **Coverage**: Enforced thresholds (57% overall, 55% EPG)

#### Code Quality Checks
- Dead code detection
- Cyclomatic complexity analysis
- Code duplication detection
- Import organization verification

**Environment Variables**:
```yaml
GO_VERSION: "1.25.0"
COVERAGE_THRESHOLD: 57
EPG_COVERAGE_THRESHOLD: 55
```

### 3. Security Workflows

#### CodeQL Analysis (`codeql.yml`)
- **Language**: Go
- **Schedule**: Weekly on Monday at 03:00 UTC
- **Queries**: security-extended
- **Upload**: Results to GitHub Security tab

#### gosec (`gosec.yml`)
- **Tool**: gosec - Go Security Checker
- **Rules**: All security rules enabled
- **Output**: SARIF format
- **Upload**: GitHub Security tab

#### govulncheck (`govulncheck.yml`)
- **Tool**: Official Go vulnerability checker
- **Database**: Go vulnerability database
- **Schedule**: Daily
- **Alerts**: GitHub Security Advisories

#### Container Security Scanning (`container-security.yml`)
- **Tool**: Trivy - Comprehensive vulnerability scanner
- **Targets**:
  - Go daemon Docker image
  - Rust transcoder Docker image
  - Filesystem secrets/misconfigurations
- **Schedule**: Daily at 2 AM UTC
- **Scopes**:
  - OS packages vulnerabilities
  - Application dependencies
  - Container configuration issues
  - Secret detection
  - SBOM generation (CycloneDX)
- **Severity Levels**: CRITICAL, HIGH, MEDIUM
- **Output Formats**:
  - SARIF (uploaded to GitHub Security)
  - Table (for PR comments)
  - CycloneDX JSON (for SBOM)
- **Key Features**:
  - Automated PR comments with vulnerability reports
  - Fails build on critical vulnerabilities
  - Configuration scanning for Docker best practices
  - SBOM artifact storage (90 days retention)

**Jobs**:
1. **scan-go-image**: Scans the main Go daemon container
2. **scan-rust-transcoder**: Scans the Rust GPU transcoder container
3. **scan-image-configuration**: Checks Docker configuration best practices
4. **scan-filesystem**: Detects secrets and misconfigurations in source code
5. **generate-sbom**: Creates and scans Software Bill of Materials

**PR Integration**:
```markdown
## 🔒 Container Security Scan: Go Daemon

<details>
<summary>Trivy Vulnerability Report</summary>

Total: 5 (CRITICAL: 1, HIGH: 2, MEDIUM: 2)
...
</details>
```

#### SBOM Generation (`sbom.yml`)
- **Tool**: Syft + Grype
- **Formats**: CycloneDX JSON/XML, SPDX JSON, Syft JSON
- **Vulnerability Scanning**: Grype with fail-on-high threshold
- **Artifacts**: Uploaded for compliance and audit trails
- **Schedule**: On code changes and daily
- **Retention**: 90 days

### 4. Docker Workflow (`docker.yml`)

**Triggers**:
- Push to main
- Pull requests
- Tag push (releases)

**Steps**:
1. **Multi-platform Build**: linux/amd64, linux/arm64
2. **BuildKit**: Advanced caching and optimization
3. **Tags**:
   - `latest` (main branch)
   - `vX.Y.Z` (semantic version tags)
   - `edge` (development)
4. **Security Scan**: Trivy container scanning
5. **Push**: Docker Hub & GitHub Container Registry

**Metadata**:
- Git commit SHA
- Build timestamp
- Go version
- Licenses

### 5. Release Workflow (`release.yml`)

**Trigger**: Tag push matching `v*.*.*`

**Steps**:
1. **Checkout**: Fetch all tags and history
2. **Go Setup**: Install Go toolchain
3. **Build**: Multi-platform binaries
   - linux/amd64
   - linux/arm64
   - darwin/amd64
   - darwin/arm64
   - windows/amd64
4. **Archive**: Create .tar.gz and .zip archives
5. **Checksums**: SHA256 checksums for all artifacts
6. **Release**: Create GitHub release with artifacts
7. **Docker**: Trigger Docker build for release tag

**Artifacts**:
- `xg2g-linux-amd64.tar.gz`
- `xg2g-linux-arm64.tar.gz`
- `xg2g-darwin-amd64.tar.gz`
- `xg2g-darwin-arm64.tar.gz`
- `xg2g-windows-amd64.zip`
- `checksums.txt`

### 6. PR Conventional Commits (`pr-conventional-commits.yml`)

**Purpose**: Enforce conventional commit messages

**Format**:
```
<type>(<scope>): <description>

[optional body]

[optional footer]
```

**Types**:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `style`: Code style changes
- `refactor`: Code refactoring
- `test`: Test changes
- `chore`: Build/tooling changes
- `ci`: CI/CD changes

**Example**:
```
feat(telemetry): Add OpenTelemetry tracing support

Implements distributed tracing with OTLP exporters for
Jaeger and Grafana Tempo.

Closes #123
```

## Local Development

### Running CI Checks Locally

#### 1. Install Tools

```bash
# golangci-lint
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

# markdownlint
npm install -g markdownlint-cli2

# pre-commit (optional)
pip install pre-commit
pre-commit install
```

#### 2. Run Checks

```bash
# Lint Go code
golangci-lint run --timeout=5m ./...

# Run tests with race detection
go test -race -cover ./...

# Lint Markdown
markdownlint-cli2 "**/*.md"

# Verify modules
go mod tidy && go mod verify

# Build
go build -v ./cmd/daemon
```

#### 3. Pre-commit Hooks (Optional)

```bash
# Install hooks
pre-commit install

# Run all hooks
pre-commit run --all-files

# Run specific hook
pre-commit run golangci-lint --all-files
```

## golangci-lint Configuration

Configuration file: [`.golangci.yml`](../.golangci.yml)

### Enabled Linters (30+)

**Core**:
- errcheck, gosimple, govet, ineffassign, staticcheck, unused

**Code Quality**:
- bodyclose, dupl, gochecknoinits, goconst, gocritic, gocyclo
- misspell, nakedret, prealloc, revive, stylecheck
- unconvert, unparam, whitespace

**Security**:
- gosec

**Go 1.18+**:
- testableexamples, usestdlibvars

**Additional**:
- bidichk, durationcheck, errname, errorlint
- forcetypeassert, nilerr, nilnil, noctx
- nolintlint, nosprintfhostport, predeclared
- sqlclosecheck, thelper, tparallel, wastedassign

### Configuration Highlights

```yaml
linters-settings:
  errcheck:
    check-type-assertions: true
    check-blank: true

  gocyclo:
    min-complexity: 15

  goconst:
    min-len: 3
    min-occurrences: 3
    ignore-tests: true

  misspell:
    locale: US
    ignore-words:
      - Picon
      - Bouquet
      - XMLTV
      - Enigma

  nakedret:
    max-func-lines: 30
```

### Exclusions

```yaml
issues:
  exclude-rules:
    # Test files: Relax some rules
    - path: _test\.go
      linters: [dupl, errcheck, gosec, goconst, noctx]

    # Main function: Allow complexity
    - path: cmd/daemon/main\.go
      linters: [gocyclo]
```

## Markdown Linting

Configuration file: [`.markdownlint.json`](../.markdownlint.json)

### Rules

- **MD001**: Heading levels increment by one
- **MD003**: ATX-style headings
- **MD004**: Dash-style unordered lists
- **MD007**: 2-space indentation for lists
- **MD013**: 120 character line length (code/tables excluded)
- **MD024**: Duplicate headings (siblings only)
- **MD033**: Allow HTML tags (details, summary, img, br, sub, sup)
- **MD034**: Bare URLs disabled
- **MD041**: First line doesn't need to be heading

### Running Locally

```bash
# Lint all Markdown files
markdownlint-cli2 "**/*.md"

# Fix auto-fixable issues
markdownlint-cli2 --fix "**/*.md"

# Lint specific file
markdownlint-cli2 README.md
```

## Dependabot Configuration

Configuration file: [`.github/dependabot.yml`](../.github/dependabot.yml)

### Automated Updates

**Go Modules**:
- Schedule: Weekly (Monday 03:00 UTC)
- Limit: 5 open PRs
- Prefix: `chore(deps)`
- Auto-assign: @ManuGH

**GitHub Actions**:
- Schedule: Weekly (Monday 03:00 UTC)
- Limit: 5 open PRs
- Prefix: `chore(ci)`

**Docker**:
- Schedule: Weekly (Monday 03:00 UTC)
- Limit: 3 open PRs
- Prefix: `chore(docker)`

### Labels

All dependency PRs are labeled:
- `dependencies`
- `go` / `ci` / `docker` (ecosystem-specific)

## Coverage Requirements

### Overall Coverage
- **Minimum**: 57%
- **Enforced by**: hardcore-ci.yml
- **Measurement**: `go tool cover -func=coverage.out`

### EPG Package
- **Minimum**: 55%
- **Enforced by**: hardcore-ci.yml
- **Critical Path**: EPG parsing and generation

### Coverage Report

```bash
# Generate coverage report
go test -coverprofile=coverage.out ./...

# View coverage by function
go tool cover -func=coverage.out

# View coverage in browser
go tool cover -html=coverage.out
```

## Performance

### CI Runtime

| Workflow | Typical Duration |
|----------|------------------|
| ci.yml | 2-3 minutes |
| hardcore-ci.yml | 5-8 minutes |
| codeql.yml | 3-5 minutes |
| docker.yml | 4-6 minutes (multi-platform) |

### Optimization Techniques

1. **Caching**:
   - Go module cache
   - Go build cache
   - Docker BuildKit cache

2. **Concurrency**:
   - Cancel in-progress runs
   - Matrix builds for multi-platform
   - Parallel job execution

3. **Selective Triggers**:
   - Path filters (e.g., `**/*.go`)
   - Branch filters
   - Schedule-based runs

## Troubleshooting

### golangci-lint Timeout

```bash
# Increase timeout
golangci-lint run --timeout=10m ./...

# Run specific linters
golangci-lint run --disable-all --enable=errcheck,govet ./...
```

### Coverage Below Threshold

```bash
# Find untested packages
go test -cover ./... | grep -v "100.0%"

# Generate detailed coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Docker Build Failures

```bash
# Build locally
docker build -t xg2g:local .

# Build with BuildKit
DOCKER_BUILDKIT=1 docker build -t xg2g:local .

# Check multi-platform support
docker buildx create --use
docker buildx build --platform linux/amd64,linux/arm64 .
```

## Best Practices

### 1. Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(api): Add /api/v1/channels endpoint
fix(epg): Correct timezone handling for EPG events
docs(telemetry): Update OpenTelemetry integration guide
chore(deps): Bump go.opentelemetry.io/otel from 1.37.0 to 1.38.0
```

### 2. Pull Requests

- **Title**: Use conventional commit format
- **Description**: Explain what and why
- **Tests**: Add tests for new features
- **Docs**: Update documentation
- **Coverage**: Don't decrease coverage

### 3. Code Quality

- **Run linters locally**: Before pushing
- **Fix warnings**: Don't ignore linter warnings
- **Add tests**: Aim for >60% coverage
- **Document**: Add package/function comments

### 4. Security

- **Scan dependencies**: Run `govulncheck`
- **Review alerts**: Check GitHub Security tab
- **Update regularly**: Keep dependencies up-to-date
- **Secrets**: Never commit secrets
- **Container scanning**: Run Trivy locally before pushing images

### 5. Container Security

Before pushing Docker images:

```bash
# Scan local image with Trivy
docker build -t xg2g:local .
trivy image --severity HIGH,CRITICAL xg2g:local

# Scan for secrets in filesystem
trivy fs --scanners secret .

# Check Dockerfile configuration
trivy config Dockerfile

# Generate SBOM
trivy image --format cyclonedx --output sbom.json xg2g:local
```

## Container Security Best Practices

### Image Hardening

The xg2g project follows security best practices for container images:

1. **Distroless Base Images**
   - Go daemon: `gcr.io/distroless/base-debian12`
   - Rust transcoder: `debian:12-slim` (requires FFmpeg)
   - Minimal attack surface
   - No shell, package manager, or unnecessary tools

2. **Non-Root User**
   ```dockerfile
   USER nonroot:nonroot
   ```
   - Containers run as unprivileged user
   - Limited filesystem access

3. **Multi-Stage Builds**
   - Separate build and runtime stages
   - Build tools not included in final image
   - Smaller image size

4. **Dependency Scanning**
   - Daily Trivy scans
   - Vulnerability database updates
   - Automated security advisories

5. **SBOM Generation**
   - CycloneDX and SPDX formats
   - Compliance and audit trails
   - Dependency tracking

### Running Containers Securely

```bash
# Run with security options
docker run -d \
  --name xg2g \
  --read-only \
  --cap-drop=ALL \
  --security-opt=no-new-privileges:true \
  --user 65532:65532 \
  -v /tmp/xg2g:/tmp:rw \
  ghcr.io/manugh/xg2g:latest

# For GPU transcoder (requires device access)
docker run -d \
  --name xg2g-transcoder \
  --read-only \
  --cap-drop=ALL \
  --cap-add=SYS_ADMIN \
  --device /dev/dri:/dev/dri \
  --security-opt=no-new-privileges:true \
  --user 65532:65532 \
  -v /tmp/transcoder:/tmp:rw \
  ghcr.io/manugh/xg2g-transcoder:latest
```

### Monitoring Security Scan Results

1. **GitHub Security Tab**
   - Navigate to: Repository → Security → Code scanning alerts
   - View Trivy, CodeQL, and Dependabot findings
   - Filter by severity: Critical, High, Medium, Low

2. **Pull Request Comments**
   - Automated vulnerability reports on PRs
   - Summary of new vulnerabilities introduced
   - Links to remediation advice

3. **Artifacts**
   - Download SBOM artifacts from Actions
   - Review detailed scan reports
   - Audit compliance requirements

### Security Scan Schedule

| Scan Type | Frequency | Trigger |
|-----------|-----------|---------|
| Container vulnerabilities | Daily at 2 AM UTC | Schedule + PR |
| Dependency vulnerabilities | Daily | Schedule + PR |
| Code security (CodeQL) | Weekly Monday 3 AM | Schedule + PR |
| Secret detection | On every commit | Push + PR |
| SBOM generation | On code changes | Push + PR + Schedule |

### Responding to Vulnerabilities

When a vulnerability is detected:

1. **Assess Severity**
   - CRITICAL: Fix immediately
   - HIGH: Fix within 7 days
   - MEDIUM: Fix within 30 days
   - LOW: Fix in next release cycle

2. **Update Dependencies**
   ```bash
   # Update Go dependencies
   go get -u ./...
   go mod tidy

   # Update Rust dependencies
   cd transcoder/
   cargo update
   ```

3. **Rebuild Images**
   ```bash
   # Rebuild with latest base images
   docker build --no-cache -t xg2g:latest .
   ```

4. **Verify Fix**
   ```bash
   # Rescan image
   trivy image xg2g:latest
   ```

5. **Document**
   - Update CHANGELOG.md
   - Reference CVE in commit message
   - Tag new release if critical

## References

- [GitHub Actions Documentation](https://docs.github.com/en/actions)
- [golangci-lint Linters](https://golangci-lint.run/usage/linters/)
- [Conventional Commits](https://www.conventionalcommits.org/)
- [Go Testing](https://go.dev/doc/tutorial/add-a-test)
- [Docker BuildKit](https://docs.docker.com/build/buildkit/)
