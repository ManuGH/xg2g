# Contributing to xg2g

Thanks for your interest in contributing! This guide will help you get started.

## Quick Start

### Prerequisites

**Required:**

- **Go 1.25+** (for Go components)
- **Docker** (for testing and deployment)
- **Make** (recommended, for convenience commands)

**Optional:**

- **Rust 1.91+** (only for `make test-transcoder` or building with native transcoder)
- **check-jsonschema** (only for `make test-schema`, install via `pip install check-jsonschema`)

### Setup Development Environment

```bash
# Clone the repository
git clone https://github.com/ManuGH/xg2g.git
cd xg2g

# Install dependencies
go mod download

# Build the project
go build -o xg2g ./cmd/daemon

# Run tests
go test ./...
```

## Development Workflow

### 1. Create a Feature Branch

```bash
git checkout -b feature/your-feature-name
```

Use prefixes:

- `feature/` - New functionality
- `fix/` - Bug fixes
- `docs/` - Documentation changes
- `refactor/` - Code refactoring
- `test/` - Test improvements
- `chore/` - Maintenance tasks

### 2. Make Your Changes

- Write clean, idiomatic Go/Rust code
- Follow existing code style
- Add tests for new functionality
- Update documentation if needed

### 3. Run Quality Checks

```bash
# Format code
go fmt ./...
gofmt -s -w .

# Run linters
go vet ./...
golangci-lint run

# Run tests
go test -v -race -cover ./...

# Run security checks (optional)
gosec ./...
```

### 4. Commit Your Changes

We use **Conventional Commits**:

```bash
git commit -m "feat(playlist): add M3U8 streaming support"
git commit -m "fix(epg): handle missing XMLTV data gracefully"
git commit -m "docs(readme): update Docker deployment examples"
```

**Format:** `<type>(<scope>): <description>`

**Types:**

- `feat` - New feature
- `fix` - Bug fix
- `docs` - Documentation
- `refactor` - Code refactoring
- `test` - Test changes
- `chore` - Maintenance
- `perf` - Performance improvements
- `ci` - CI/CD changes

### 5. Push and Create Pull Request

```bash
git push origin feature/your-feature-name
```

Then open a PR on GitHub with:

- Clear description of changes
- Link to related issues
- Screenshots/logs (if applicable)

## Testing

The project uses **Make targets** as the canonical interface for testing.
These targets mirror the CI pipeline jobs, ensuring local development matches CI behavior.

### Standard Test Run (Recommended)

For most development work, use:

```bash
make test
```

This runs all unit and integration tests with the **stub transcoder** (no Rust dependency required).
It includes:

- All package tests (`go test ./...`)
- Race detection (`-race`)
- Coverage reporting (`-coverprofile=coverage.out`)

**Internally equivalent to:**

```bash
go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
```

### Transcoder Tests (Rust FFI)

To test the **real Rust-based transcoder**:

```bash
make test-transcoder
```

This target:

1. Builds the Rust library (`libxg2g_transcoder.a`)
2. Sets `CGO_ENABLED=1`
3. Runs tests with `-tags=transcoder`

**Prerequisites:**

- Rust toolchain (`rustup` recommended)
- C toolchain/linker
- FFmpeg development headers (if required locally)

**Manual alternative:**

```bash
cd transcoder && cargo build --release
CGO_ENABLED=1 go test -tags=transcoder ./internal/transcoder -v
```

### Schema Validation Tests

JSON schema tests are isolated from standard tests and require the external tool `check-jsonschema`:

```bash
# Install check-jsonschema
pip install check-jsonschema

# Run schema tests
make test-schema
```

This runs `go test -tags=schemacheck ./internal/config` - verifying config validation logic and JSON schema correctness.

### Additional Test Commands

```bash
# Run tests with race detection only
make test-race

# Generate detailed coverage report
make test-cover  # Creates coverage.html

# Run fuzzing tests (EPG package)
make test-fuzz

# Complete test suite (unit + race + fuzz)
make test-all
```

### CI Alignment

The CI pipeline uses the same Make targets:

| CI Job | Local Command | Purpose |
|--------|---------------|---------|
| `unit-tests` | `make test` | Fast tests, stub transcoder |
| `schema-validation` | `make test-schema` | Config schema validation |
| `transcoder-tests` | `make test-transcoder` | Rust FFI tests (nightly/main only) |
| `lint` | `make lint` | Go/Markdown/OpenAPI linting |
| `integration-tests` | End-to-end daemon tests | Full stack validation |

**If `make test` passes locally, the core CI jobs should pass.**

### Integration Tests

```bash
# Run integration tests (requires building binaries)
go test -tags=integration -v ./test/integration/...

# Or use Docker
docker build -t xg2g:test .
docker run --rm xg2g:test
```

## Common Make Commands

| Command | Description |
|---------|-------------|
| `make build` | Build main daemon binary (stub transcoder) |
| `make build-rust` | Build Rust transcoder library |
| `make test` | Run unit tests (stub transcoder, fast) |
| `make test-transcoder` | Run tests with Rust transcoder (requires Rust) |
| `make test-schema` | Run JSON schema validation tests |
| `make test-race` | Run tests with race detection |
| `make test-cover` | Run tests with coverage report |
| `make lint` | Run golangci-lint |
| `make lint-fix` | Run golangci-lint with automatic fixes |
| `make docker` | Build Docker image locally |
| `make dev` | Run daemon from source with `.env` config |
| `make up` | Start docker-compose.yml stack |
| `make help` | Show all available commands |

## Pre-Commit Hooks

Install pre-commit hooks to validate changes locally:

```bash
pip install pre-commit
pre-commit install
```

Hooks validate:

- Go formatting (`gofmt`)
- YAML formatting and linting
- Health check endpoint usage
- File permissions and merge conflicts

## Code Standards

### Go

- Follow [Effective Go](https://go.dev/doc/effective_go)
- Use `go fmt` for formatting
- Keep functions short and focused
- Add godoc comments for exported functions
- Handle errors explicitly (no silent failures)

**Example:**

```go
// FetchChannels retrieves the channel list from the OpenWebif API.
// Returns an error if the API is unreachable or returns invalid data.
func FetchChannels(baseURL string) ([]Channel, error) {
    resp, err := http.Get(baseURL + "/api/getchannels")
    if err != nil {
        return nil, fmt.Errorf("failed to fetch channels: %w", err)
    }
    defer resp.Body.Close()

    // ... rest of implementation
}
```

### Rust (Transcoder Components)

- Follow [Rust API Guidelines](https://rust-lang.github.io/api-guidelines/)
- Use `rustfmt` for formatting
- Handle errors with `Result<T, E>`
- Add documentation comments (`///`)

## Security

### Reporting Vulnerabilities

**DO NOT** open public issues for security vulnerabilities.

Instead, report to: **[security contact email]** or use [GitHub Security Advisories](https://github.com/ManuGH/xg2g/security/advisories).

### Security Best Practices

- Never commit secrets (API keys, passwords)
- Validate all user inputs
- Use parameterized queries for databases
- Follow OWASP Top 10 guidelines
- Run security scans: `gosec ./...`

## Pull Request Guidelines

### Before Submitting

- ✅ All tests pass
- ✅ Code is formatted
- ✅ No linter warnings
- ✅ Documentation updated
- ✅ Conventional commit messages
- ✅ No sensitive data in commits

### PR Description Template

```markdown
## Summary
Brief description of what this PR does.

## Changes
- Change 1
- Change 2

## Testing
- [ ] Unit tests added/updated
- [ ] Integration tests pass
- [ ] Manual testing performed

## Related Issues
Closes #123
```

### Review Process

1. Automated checks run (CI, tests, security scans)
2. Code review by maintainers
3. Address feedback
4. Approval and merge

**Typical response time:** 1-3 days

## Project Structure

```
xg2g/
├── cmd/                     # Main applications (daemon, tools)
│   └── daemon/              # Main application entry point
├── internal/                # Private application code
│   ├── api/                 # HTTP API handlers
│   ├── playlist/            # M3U playlist generation
│   ├── epg/                 # EPG/XMLTV handling
│   ├── hdhr/                # HDHomeRun emulation
│   └── stream/              # Stream proxy
├── pkg/                     # Public library code (if any)
├── api/                     # OpenAPI/Swagger specs
├── webui/                   # React frontend
├── deploy/                  # Deployment configs (Docker, K8s, Helm)
│   ├── docker/              # Specialized Dockerfiles
│   ├── helm/                # Helm charts
│   └── kubernetes/          # K8s manifests
├── docs/                    # Documentation
├── scripts/                 # Build and maintenance scripts
├── .github/
│   └── workflows/           # Core CI/CD pipelines
├── Dockerfile               # Main Docker image
└── docker-compose.yml       # Standard deployment
```

## Common Tasks

### Add a New API Endpoint

1. Add handler in `internal/api/handlers.go`
2. Register route in `internal/api/router.go`
3. Add tests in `internal/api/handlers_test.go`
4. Update API documentation

### Add a New Environment Variable

1. Add to `internal/config/config.go`
2. Document in `README.md`
3. Add validation logic
4. Update `docker-compose.yml` example

### Add a New Dependency

```bash
# Go dependencies
go get github.com/some/package
go mod tidy

# Verify it works
go mod verify
```

## Getting Help

- **Questions:** Open a [Discussion](https://github.com/ManuGH/xg2g/discussions)
- **Bug Reports:** Open an [Issue](https://github.com/ManuGH/xg2g/issues)
- **Feature Requests:** Open an [Issue](https://github.com/ManuGH/xg2g/issues) with `enhancement` label

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).

---

**Thank you for contributing to xg2g!**
