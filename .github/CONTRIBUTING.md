# Contributing to xg2g

Welcome! This guide will help you get started with contributing to xg2g.

## Community Channels

- Questions and ideas: [GitHub Discussions](https://github.com/ManuGH/xg2g/discussions)
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

2.  **Bootstrap the development workspace**:
    ```bash
    make install
    make dev-tools
    make doctor
    ```

3.  **Start the local container environment**:
    ```bash
    make start
    ```

4.  **Run tests**:
    ```bash
    make test
    ```

## Project Structure

The project is organized into a monorepo with a clear separation between backend and frontend:

- `backend/`: Contains all Go source code, internal packages, and backend-specific scripts.
- `apps/`: Contains application frontends, including the Web UI in `apps/webui/`.
- `infra/`: Contains Docker, systemd, reverse-proxy, and monitoring infrastructure.
- `mk/`: Modular Makefile fragments.
- `docs/`: Project documentation.

## Development Workflow

### Backend (Go)
The backend is located in [backend/](backend/).
To run the daemon directly:
```bash
make dev
```

`make dev` is a single foreground run. Use `make start` when you need the
standard local container stack.

### Frontend (WebUI)
Located in [apps/webui/](apps/webui/).
```bash
make dev-ui
```

## Quality Assurance & Verification

Before submitting a Pull Request, please ensure:
- Primary PR gate bundle passes: `make ci-pr`
- Unit tests pass: `make test`
- All quality gates pass: `make quality-gates`
- Pre-push guard passes: `make pre-push`
- Document rendering is up to date (no drift): `make verify-docs-compiled`

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

    Release copy is standardized through:
    - [docs/release/RELEASE_TEMPLATE.md](docs/release/RELEASE_TEMPLATE.md)
    - [docs/release/GITHUB_PRESENCE_COPY.md](docs/release/GITHUB_PRESENCE_COPY.md)

## Pull Request Checklist

- [ ] New features include tests.
- [ ] Documentation is updated (if applicable).
- [ ] No regression markers (`FIXME`, `TODO`) left in production code.
- [ ] Commit messages follow the project convention.

## Code of Conduct

Please read and follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## License
By contributing, you agree that your contributions will be licensed under the project's [LICENSE](LICENSE).
