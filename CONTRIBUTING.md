# Contributing to xg2g

Thanks for your interest in contributing to **xg2g**!
This project is in an early stage. Pull requests and issues are welcome.

## Development Setup

```bash
# Clone the repository
git clone https://github.com/ManuGH/xg2g.git
cd xg2g

# Download dependencies
go mod download

# Build
go build ./cmd/daemon

# Run locally
./xg2g
```

## Language Policy

**This project follows an English-only policy** for all communication and documentation:

- **Issues and Pull Requests**: Must be written in English (titles, descriptions, comments)
- **Code Comments**: English only
- **Documentation**: All `.md` files must be in English
- **Commit Messages**: English preferred

This ensures accessibility for the global open-source community and maintains consistency.

## Code Quality Standards

Before submitting a pull request, ensure your code passes all quality checks:

```bash
# Run tests
go test ./... -race

# Run linter
make lint

# Run build
go build ./cmd/daemon
```

## Submission Guidelines

1. **Issues**: Use clear, descriptive titles in English
2. **Pull Requests**: Include clear description of changes
3. **Testing**: Add tests for new functionality
4. **Documentation**: Update relevant documentation
