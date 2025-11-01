# Contributing to xg2g

Thanks for your interest in contributing! This guide will help you get started.

## Quick Start

### Prerequisites

- **Go 1.25+** (for Go components)
- **Rust 1.82+** (for transcoder components)
- **Docker** (for testing)
- **Make** (optional, for convenience commands)

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

### Unit Tests

```bash
# Run all tests
go test ./...

# Run specific package tests
go test ./internal/playlist

# Run with coverage
go test -cover ./...

# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Integration Tests

```bash
# Docker integration tests
make docker-integration-test

# Or manually:
docker build -t xg2g:test .
docker run --rm xg2g:test
```

### Fuzzing

```bash
# Run fuzz tests
go test -fuzz=FuzzPlaylistParser -fuzztime=30s ./internal/playlist
```

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
├── cmd/
│   └── daemon/          # Main application entry point
├── internal/
│   ├── api/             # HTTP API handlers
│   ├── playlist/        # M3U playlist generation
│   ├── epg/             # EPG/XMLTV handling
│   ├── hdhr/            # HDHomeRun emulation
│   └── stream/          # Stream proxy
├── pkg/                 # Public packages (if any)
├── .github/
│   └── workflows/       # CI/CD pipelines
├── Dockerfile           # Docker image definition
└── docker-compose.yml   # Docker Compose example
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
