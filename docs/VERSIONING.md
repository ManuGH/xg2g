# Semantic Versioning Guidelines

This project adheres to [Semantic Versioning (SemVer)](https://semver.org/).

## 1. The Standard

The version format is `MAJOR.MINOR.PATCH` (e.g., `1.4.2`).

| Part  | When to increment? | Meaning |
|-------|--------------------|---------|
| **MAJOR** | Breaking Changes | Incompatible API changes |
| **MINOR** | New Features | Backwards-compatible functionality |
| **PATCH** | Bug Fixes | Backwards-compatible bug fixes |

### Examples

- **Bug fix**: `1.2.3` -> `1.2.4`
- **New feature**: `1.2.4` -> `1.3.0`
- **Breaking API change**: `1.3.0` -> `2.0.0`
- **New major architecture**: `2.x` -> `3.0.0`

## 2. What 1.0.0 Means

Reaching `1.0.0` signifies **API Stability**.

- You pledge compatibility.
- Users can expect that `v1` updates will not break their setup.
- Anything before `1.0.0` (`0.x.x`) is considered experimental/unstable.

## 3. When to Bump MAJOR (2.0, 3.0)

Increment MAJOR version **ONLY** for genuine breaking changes.

**Valid Reasons:**

- Configuration file format changes (e.g., YAML structure change).
- API endpoints removed or renamed.
- Default behavior changes that require user intervention.
- CLI flags changed or removed.
- Data migration required.

**Invalid Reasons (Do NOT bump Major):**

- Internal refactoring.
- Performance improvements.
- Large feature additions (without breaking changes).

## 4. Pre-Releases

Use pre-release tags for unstable versions: `alpha`, `beta`, `rc` (Release Candidate).

- `2.0.0-alpha.1`: Incomplete, unstable.
- `2.0.0-beta.2`: Feature-complete, testing phase.
- `2.0.0-rc.1`: Release candidate, considered stable unless bugs found.
- `2.0.0`: Final stable release.

## 5. GitHub Release Workflow

1. **Tagging**
    Always use the `v` prefix.

    ```bash
    git tag -a v1.2.0 -m "Release v1.2.0"
    git push origin v1.2.0
    ```

2. **Releasing**
    - Create a GitHub Release from the tag.
    - generate specific binaries.
    - Attach assets (binaries, checksums).

## 6. Changelog

Maintain a `CHANGELOG.md` following [Keep a Changelog](https://keepachangelog.com/).

### Format

```markdown
## [2.0.0] - 2025-01-10
### Breaking
- Changed config format from YAML to TOML.

### Added
- New Metrics API endpoint.

### Fixed
- Resolved memory leak in worker pool.
```

## 7. Versioning Strategy

- **main/master**: Always stable.
- **develop**: Integration branch for next MINOR release.
- **release/x.y**: Stabilization branches for RCs.

**Summary Rule:**

- **MAJOR** breaks trust (requires migration).
- **MINOR** adds value (safe update).
- **PATCH** fixes problems (safe update).

---

## 🤖 Instructions for AI Assistants

When modifying code or proposing releases, strictly follow these rules:

1. **Analyze Impact**: Check if changes are breaking (config, API, CLI).
2. **Suggest Version**: Propose the correct SemVer bump (Patch/Minor/Major) based on impact.
3. **Update Changelog**: Always verify `CHANGELOG.md` is updated under `[Unreleased]` or the new version.
4. **No Soft Breaks**: Do not hide breaking changes in Minor releases. If it breaks, it is Major.
