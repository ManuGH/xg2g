# Contributing to xg2g

Welcome! This guide will help you get started with contributing to xg2g.

## Community Channels

- Questions and ideas: GitHub Issues with label `question`
- Feature proposals: GitHub Issues with label `enhancement`
- Bugs and feature requests: GitHub Issues
- Security reports: [GitHub Security Advisories](https://github.com/ManuGH/xg2g/security/advisories/new)

If you want to start with a small task, look for issues labeled:

- `good first issue`
- `help wanted`

## Quick Start (5 Minutes)

1.  **Clone the repository**:
    ```bash
    git clone https://github.com/ManuGH/xg2g.git
    cd xg2g
    ```

2.  **Initialize Go Workspace**:
    ```bash
    go work init ./backend
    ```

3.  **Start the development environment**:
    ```bash
    make dev
    ```

4.  **Run tests**:
    ```bash
    make test
    ```

## Project Structure

The project is organized into a monorepo with a clear separation between backend and frontend:

- `backend/`: Contains all Go source code, internal packages, and backend-specific scripts.
- `frontend/`: Contains the Web UI (located in `frontend/webui/`).
- `infrastructure/`: Docker Compose files and monitoring configurations.
- `mk/`: Modular Makefile fragments.
- `docs/`: Project documentation.

## Development Workflow

### Backend (Go)
The backend is located in [backend/](backend/).
To run the daemon directly:
```bash
cd backend
go run ./cmd/daemon
```
Or via the root Makefile (recommended):
```bash
make dev
```

### Frontend (WebUI)
Located in [frontend/webui/](frontend/webui/).
```bash
cd frontend/webui
npm ci
npm run dev
```

## Quality Assurance

Before submitting a Pull Request, please ensure:
- All tests pass: `make test`
- Linting is clean: `make lint`
- Quality gates pass: `make quality-gates`

## Creating a New Release

1.  **Tag and Push**:
    ```bash
    make release version=vX.Y.Z
    ```
    This command runs all quality gates and, if successful, creates and pushes a git tag.

2.  **Automated Processing**: 
    The GitHub Actions [release workflow](.github/workflows/release.yml) will automatically:
    - Generate release notes from commit history.
    - Build binaries for Linux, macOS, and Windows.
    - Build and push multi-architecture Docker images to GHCR.
    - Create a GitHub Release with all artifacts.

## Pull Request Checklist

- [ ] New features include tests.
- [ ] Documentation is updated (if applicable).
- [ ] No regression markers (`FIXME`, `TODO`) left in production code.
- [ ] Commit messages follow the project convention.

## Code of Conduct

Please read and follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## License
By contributing, you agree that your contributions will be licensed under the project's [LICENSE](LICENSE).
