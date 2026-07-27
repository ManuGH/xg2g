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
- `frontend/`: Contains the Web UI (located in `frontend/webui/`).
- `infrastructure/`: Docker Compose files and monitoring configurations.
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
Located in [frontend/webui/](frontend/webui/).
```bash
make dev-ui
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

    Release copy is standardized through:
    - [docs/release/RELEASE_TEMPLATE.md](docs/release/RELEASE_TEMPLATE.md)
    - [docs/release/GITHUB_PRESENCE_COPY.md](docs/release/GITHUB_PRESENCE_COPY.md)

## Pull Request Checklist

- [ ] New features include tests.
- [ ] Documentation is updated (if applicable).
- [ ] No regression markers (`FIXME`, `TODO`) left in production code.
- [ ] Commit messages follow the project convention.

## How Pull Requests Are Gated

Branch protection on `main` requires four status checks. They are deliberately
*aggregate* contexts, not individual job names — requiring a job name couples the
protection rules to job titles, so renaming or splitting a job silently turns its
required context into a permanent "Expected — waiting for status" block:

| Required check | Covers |
| --- | --- |
| `CI / Gate` | every job in `ci.yml` (PR gate bundle, race detector) |
| `Required Gates` | WebUI build + browser smoke |
| `Go Coverage Report` | coverage run and threshold |
| `Audio Contract / ffmpeg replay` | replays the daemon's real ffmpeg argument vector against a synthetic DVB fixture |

None of these workflows filter on `paths:`. A filtered workflow does not run at all,
so it never reports a status, and a required check that never reports blocks the PR
forever. Each job resolves its own scope instead
(`backend/scripts/ci/resolve-workflow-scope.sh`) and reports a green no-op in seconds
when the diff does not touch its area.

### `strict` is off, deliberately

Required checks run with `strict: false`, so a PR does not have to be rebased onto the
latest `main` before merging. This trades a little safety for a lot of churn: with
`strict: true` every merge invalidates every other open PR's checks, and this repo
stacks PRs on top of each other. The residual risk is real though — two PRs that are
individually green can break `main` together. If merge frequency rises, the answer is a
merge queue rather than `strict: true`, because a queue tests the actual merge result
without serialising every author.

## Code of Conduct

Please read and follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## License
By contributing, you agree that your contributions will be licensed under the project's [LICENSE](LICENSE).
