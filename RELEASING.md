# Release Process

This document describes how to create releases for xg2g.

## Automated Releases with GoReleaser

Releases are fully automated using GoReleaser. Simply push a new version tag:

```bash
# Create and push a new version tag
git tag v1.6.0
git push origin v1.6.0
```

This will automatically:
- Build multi-platform binaries (Linux, macOS, Windows for amd64/arm64)
- Generate checksums and SBOM
- Sign artifacts with Cosign
- Create a GitHub Release with changelog
- Upload all assets

## What Gets Released

### 1. CLI Binaries (nogpu builds)
- **Linux:** amd64, arm64
- **macOS:** amd64, arm64 (Apple Silicon)
- **Windows:** amd64

**Note:** These binaries are built **without audio transcoding support** (nogpu tag).
- ✅ M3U playlist generation
- ✅ XMLTV EPG
- ✅ HDHomeRun emulation
- ❌ Audio transcoding (AC3/MP2 → AAC)

For full functionality, use Docker images.

### 2. Docker Images (full builds)
Docker images with complete Rust+Go transcoding support are built automatically on:
- Every push to `main` → `ghcr.io/manugh/xg2g:latest`
- Version tags → `ghcr.io/manugh/xg2g:v1.6.0`

Docker images include:
- ✅ All features
- ✅ Rust audio transcoder
- ✅ iOS/Safari audio support
- ✅ Multi-arch (linux/amd64, linux/arm64)

## Release Checklist

Before creating a release:

1. **Update CHANGELOG.md**
   ```bash
   # Add new section for version
   ## [1.6.0] - 2025-01-15
   ### Added
   - New feature X
   ### Fixed
   - Bug Y
   ```

2. **Ensure all tests pass**
   ```bash
   go test ./...
   ```

3. **Update version in documentation** (if needed)
   - README.md
   - docker-compose.yml examples

4. **Create and push tag**
   ```bash
   git tag -a v1.6.0 -m "Release v1.6.0"
   git push origin v1.6.0
   ```

5. **Monitor GitHub Actions**
   - Watch the GoReleaser workflow: https://github.com/ManuGH/xg2g/actions/workflows/goreleaser.yml
   - Verify Docker workflow: https://github.com/ManuGH/xg2g/actions/workflows/docker.yml

6. **Verify the release**
   - Check GitHub Releases: https://github.com/ManuGH/xg2g/releases
   - Test a binary download
   - Test Docker image: `docker pull ghcr.io/manugh/xg2g:v1.6.0`

## Versioning

We follow [Semantic Versioning](https://semver.org/):

- **v1.6.0** - Major.Minor.Patch
- **v1.6.0-beta.1** - Pre-release
- **v1.6.0-rc.1** - Release candidate

### When to bump versions:

- **Major (v2.0.0):** Breaking changes, incompatible API changes
- **Minor (v1.6.0):** New features, backward-compatible
- **Patch (v1.5.1):** Bug fixes, backward-compatible

## Manual Release (Not Recommended)

If you need to create a release manually (e.g., for testing):

```bash
# Install GoReleaser locally
go install github.com/goreleaser/goreleaser/v2@latest

# Test release locally (dry run)
goreleaser release --snapshot --clean

# Check generated artifacts
ls dist/
```

## Troubleshooting

### Release workflow failed
1. Check GitHub Actions logs
2. Common issues:
   - Tag already exists: Delete and recreate
   - Tests failing: Fix tests first
   - Go version mismatch: Update go.mod

### Docker build failed
1. Check Docker workflow logs
2. Common issues:
   - Rust dependencies: Check Dockerfile
   - FFmpeg version: Update Alpine packages

### Missing artifacts
1. Check .goreleaser.yml configuration
2. Verify build matrix (OS/Arch combinations)
3. Check GitHub Actions permissions

## Release Artifacts

Each release includes:

- **Binaries:**
  - `xg2g_v1.6.0_linux_amd64.tar.gz`
  - `xg2g_v1.6.0_linux_arm64.tar.gz`
  - `xg2g_v1.6.0_darwin_amd64.tar.gz`
  - `xg2g_v1.6.0_darwin_arm64.tar.gz`
  - `xg2g_v1.6.0_windows_amd64.zip`

- **Checksums:**
  - `checksums.txt` (SHA256)

- **SBOM:**
  - `xg2g_v1.6.0_sbom.spdx.json`

- **Signatures:**
  - Cosign signatures (`.sig` files)

## Security

All artifacts are:
- ✅ Signed with Cosign (keyless)
- ✅ Include SBOM (SPDX format)
- ✅ Built with reproducible builds
- ✅ Scanned for vulnerabilities (CodeQL, Trivy)

Verify signatures:
```bash
# Install cosign
go install github.com/sigstore/cosign/v2/cmd/cosign@latest

# Verify release artifact
cosign verify-blob \
  --certificate-identity-regexp="https://github.com/ManuGH/xg2g" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  --signature=checksums.txt.sig \
  checksums.txt
```

---

**Need help?** Open a [Discussion](https://github.com/ManuGH/xg2g/discussions) or [Issue](https://github.com/ManuGH/xg2g/issues).
