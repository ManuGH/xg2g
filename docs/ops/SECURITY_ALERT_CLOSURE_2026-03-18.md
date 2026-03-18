# Security Alert Closure Pack

**Date:** 2026-03-18
**Status:** Pending fresh scan evidence

## Scope

- Runtime image hardening (`Dockerfile`, `Dockerfile.ffmpeg-base`, `infrastructure/docker/Dockerfile.release`)
- Distroless non-root posture (`Dockerfile.distroless`)
- Path confinement (`backend/internal/platform/fs/confinement.go`)
- Workflow token minimization (`.github/workflows/*.yml`)

## Root Cause Summary

- Container fixes were code-only until rebuilt and re-verified against fresh image layers.
- Path confinement needed proof across lexical traversal, symlink escape, symlink chains, and nonexistent leaves.
- Workflow permission hardening reduced token scope, but scanner closure still needed a reproducible rerun path.

## Fixes Under Verification

- Runtime images now declare and use explicit non-root users.
- Writable runtime directories are created and owned by the runtime UID/GID.
- Distroless copies the binary with non-root ownership and declares a non-root user explicitly.
- Path confinement now performs a lexical under-root check before `Lstat`, `Stat`, or `EvalSymlinks`.
- Workflow defaults are narrowed to read-only where broad write access was unnecessary.

## Mechanical Verification

### Local closure bundle

```bash
make security-closure
```

What it proves:

- targeted confinement regression matrix passes
- fresh no-cache image builds complete
- production image runs as `10001:10001`
- production image can execute `xg2g` and `healthcheck` as non-root
- production image exposes writable runtime paths for temp, sessions, and recordings
- FFmpeg wrappers are executable under the non-root runtime user
- distroless image runs as `65532:65532`
- distroless entrypoint executes under its default non-root user

Optional stricter rebuild:

```bash
XG2G_VERIFY_PULL=1 make security-closure
```

Use that in a connected environment when you need fresh base-image pulls in addition to no-cache rebuilds.

### Scanner reruns

Run these after the patch lands on the branch under review:

1. GitHub CodeQL workflow
2. GitHub Trivy workflow with enforcement enabled
3. GitHub Scorecard workflow

These remain the source of truth for scanner-native evidence and alert state transitions.

### Deploy-time runtime proof

For the production runtime, use the existing host-attached proofs after rollout:

```bash
sudo /srv/xg2g/scripts/run-service-smoke.sh
sudo /srv/xg2g/scripts/verify-post-deploy-playback.sh
```

Those prove health/readiness, real live playback, and forced-transcode behavior under the actual deployed container rather than an isolated build-stage container.

## Evidence Checklist

Attach the following to the PR or closure note:

- `make security-closure` output
- image build logs showing `--no-cache`
- `docker inspect` user proof for runtime and distroless images
- successful writable-path probe output for `/var/lib/xg2g/tmp`, `/var/lib/xg2g/sessions`, `/var/lib/xg2g/recordings`
- successful FFmpeg wrapper execution proof
- targeted `go test ./internal/platform/fs` output
- CodeQL run URL
- Trivy run URL and result summary
- Scorecard run URL and result summary
- post-deploy smoke and playback verifier output if the change is deployed

## Residual Risk And Non-Goals

- The confinement fix is fail-closed before path resolution and after resolved-parent checks, but it is not an atomic open primitive. TOCTOU after validation remains out of scope for this slice.
- The distroless image proof is limited to buildability, configured user, and entrypoint execution. Full FFmpeg/playback runtime proof applies to the production image, not the minimal distroless artifact.
- Local verification does not replace GitHub-native CodeQL or Scorecard evidence.
