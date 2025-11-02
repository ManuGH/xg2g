# Release Checklist

Pre-release verification checklist for xg2g version tags.

## Pre-Release Verification

### 1. Code Quality

- [ ] All CI checks passing on `main` branch
- [ ] `golangci-lint` passing without warnings
- [ ] `go vet` clean
- [ ] Coverage ≥ 55% overall, EPG module ≥ 55%
- [ ] No known security vulnerabilities (`govulncheck`)
- [ ] All tests passing (unit, integration, fuzzing)

### 2. Dependency Health

```bash
# Verify all dependencies
go mod verify
go mod tidy
git diff --exit-code go.mod go.sum

# Check for vulnerabilities
govulncheck ./...
```

### 3. Documentation

- [ ] README.md updated with new features
- [ ] CHANGELOG.md updated with version changes
- [ ] API documentation current
- [ ] Configuration examples updated
- [ ] Migration guides (if breaking changes)

### 4. Version Bump

```bash
# Tag format: v1.2.3
VERSION="v1.x.x"  # Update with actual version

# Create annotated tag
git tag -a "$VERSION" -m "Release $VERSION

Features:
- Feature 1
- Feature 2

Fixes:
- Fix 1
- Fix 2

Breaking Changes:
- None / List changes

Full changelog: https://github.com/ManuGH/xg2g/compare/v1.x.x...$VERSION"
```

## Build Verification

### 5. Local Build Test

```bash
# Test AMD64 builds (all CPU levels)
docker buildx build --platform linux/amd64 --build-arg GO_AMD64_LEVEL=v1 .
docker buildx build --platform linux/amd64 --build-arg GO_AMD64_LEVEL=v2 .
docker buildx build --platform linux/amd64 --build-arg GO_AMD64_LEVEL=v3 .

# Test ARM64 build (QEMU, slow)
docker buildx build --platform linux/arm64 .
```

### 6. Push Tag

```bash
# Push tag to trigger release builds
git push origin "$VERSION"

# Monitor CI workflows
gh run list --workflow=docker-multi-cpu.yml --limit 1
gh run watch  # Watch the latest run
```

Expected workflows:
- `docker-multi-cpu`: AMD64 (v1, v2, v3) + ARM64 builds (~60-90 min total)
- `docker`: Distroless variant
- `CI`: All quality checks
- `Hardcore CI/CD Pipeline`: Full validation

## Post-Build Verification

### 7. Image Availability

```bash
# Check all expected tags exist
REPO="ghcr.io/manugh/xg2g"

docker pull "$REPO:latest"
docker pull "$REPO:$VERSION"
docker pull "$REPO:v1-compat"
docker pull "$REPO:v3-performance"
docker pull "$REPO:$VERSION-arm64"

# Verify multi-arch manifest
docker buildx imagetools inspect "$REPO:latest"
# Expected: linux/amd64, linux/arm64
```

### 8. SBOM & Provenance

```bash
# Verify attestations (requires cosign)
cosign verify "$REPO:latest" 2>&1 | head -20

# Check SBOM
docker buildx imagetools inspect "$REPO:latest" --format "{{json .SBOM}}"

# Check provenance
docker buildx imagetools inspect "$REPO:latest" --format "{{json .Provenance}}"
```

### 9. Smoke Test (AMD64)

```bash
# Quick health check
docker run --rm "$REPO:$VERSION" --version

# Start with minimal config
docker run -d --name xg2g-test \
  -p 8080:8080 \
  -e XG2G_OWI_BASE=http://receiver.local \
  -e XG2G_BOUQUET=Favourites \
  "$REPO:$VERSION"

# Check health
curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/api/status | jq

# Cleanup
docker stop xg2g-test && docker rm xg2g-test
```

### 10. Smoke Test (ARM64)

**If ARM64 host available:**

```bash
# On ARM64 machine (Raspberry Pi, Apple M-series, ARM server)
docker run --rm "$REPO:$VERSION-arm64" --version

# Verify correct architecture
docker run --rm "$REPO:$VERSION-arm64" uname -m
# Expected: aarch64

# Quick health check
docker run -d --name xg2g-arm64-test \
  -p 8080:8080 \
  -e XG2G_OWI_BASE=http://receiver.local \
  -e XG2G_BOUQUET=Favourites \
  "$REPO:$VERSION-arm64"

curl -fsS http://localhost:8080/healthz

docker stop xg2g-arm64-test && docker rm xg2g-arm64-test
```

**If no ARM64 host:**
- Verify CI build logs for ARM64 job success
- Check canary build artifacts from previous nightly run

## Release Publication

### 11. GitHub Release

```bash
# Create GitHub release (auto-generates from tag annotation)
gh release create "$VERSION" \
  --title "xg2g $VERSION" \
  --notes "See [CHANGELOG.md](https://github.com/ManuGH/xg2g/blob/main/docs/CHANGELOG.md) for details." \
  --verify-tag

# Alternatively, use GitHub UI for richer release notes
```

Release notes should include:
- 🎉 **New Features**
- 🐛 **Bug Fixes**
- ⚡ **Performance Improvements**
- 🔒 **Security Updates**
- 📖 **Documentation**
- ⚠️ **Breaking Changes** (if any)

### 12. Update Deployment Examples

```bash
# Update docker-compose.production.yml with new version
sed -i "s|ghcr.io/manugh/xg2g:v.*|ghcr.io/manugh/xg2g:$VERSION|g" \
  docker-compose.production.yml

# Commit and push
git add docker-compose.production.yml
git commit -m "chore: update production example to $VERSION"
git push origin main
```

## Post-Release Monitoring

### 13. Monitor First 24 Hours

- [ ] GitHub Issues: Watch for new bug reports
- [ ] Docker Hub pulls: Verify download activity
- [ ] CI/CD: Check nightly canary builds still passing
- [ ] Discussions: Monitor user feedback

### 14. Documentation Updates

- [ ] Update Helm chart version (if applicable)
- [ ] Update API documentation (if schema changed)
- [ ] Announce in Discussions (if major release)

## Rollback Procedure

If critical issues discovered:

```bash
# Option 1: Delete tag (if no users pulled yet)
git tag -d "$VERSION"
git push origin :refs/tags/"$VERSION"
gh release delete "$VERSION" --yes

# Option 2: Create hotfix patch release
# Example: v1.2.3 → v1.2.4
# Fix issue on main, create new tag

# Option 3: Document known issue in release notes
gh release edit "$VERSION" --notes "⚠️ Known Issue: ..."
```

## CI/CD Build Times Reference

| Build Type | Duration | Notes |
|------------|----------|-------|
| **Main branch (AMD64 only)** | ~2-3 min | v1 + v2 + v3 parallel |
| **Release AMD64** | ~2-3 min | Same as main |
| **Release ARM64** | 60-90 min | QEMU emulation |
| **Total Release** | ~60-90 min | AMD64 + ARM64 sequential |

**Future (Option A activated):**
- ARM64 cross-compile: 5-10 min
- Total release: ~10-15 min

## Troubleshooting

### Build Failures

**AMD64 build fails:**
```bash
# Check workflow logs
gh run view --log-failed

# Common issues:
# - Rust compile errors → Check transcoder/
# - Go build errors → Check cmd/daemon/
# - Linter failures → Run golangci-lint locally
```

**ARM64 build timeout:**
```bash
# QEMU builds can timeout (>2 hours)
# - Check if cargo build is stuck
# - Verify FFmpeg dependencies available
# - Consider activating Option A (cross-compile)
```

### Image Pull Failures

```bash
# Verify image exists
gh run view --job <job-id>

# Check GHCR package visibility
gh api /user/packages/container/xg2g/versions

# Re-tag if manifest incorrect
docker buildx imagetools create \
  -t ghcr.io/manugh/xg2g:$VERSION \
  ghcr.io/manugh/xg2g:sha-abc1234
```

---

## Version Numbering

Follow [Semantic Versioning](https://semver.org/):

- **MAJOR** (v2.0.0): Breaking changes
- **MINOR** (v1.x.0): New features, backward-compatible
- **PATCH** (v1.2.x): Bug fixes, backward-compatible

**Pre-release tags:**
- `v1.2.0-rc.1`: Release candidate
- `v1.2.0-alpha.1`: Alpha preview
- `v1.2.0-beta.1`: Beta preview
