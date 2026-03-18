# Release Output Contract

Canonical publication contract for `xg2g` releases. This defines which release
artifacts are normatively guaranteed, which files are only release governance
inputs, and which outputs are explicitly outside the published contract.

## Policy

- Missing normative release output is a blocker.
- `unexpected published output` is a blocker.
- Release output changes require updating this document, the verifier, and the
  release configuration in the same slice.
- The canonical version source is `backend/VERSION` in tag form (`vX.Y.Z`).
- GitHub release archive names use GoReleaser `{{ .Version }}` semantics
  (`X.Y.Z` without the leading `v`).
- Registry publication uses tag semantics (`ghcr.io/manugh/xg2g:vX.Y.Z`).

## Normative Published Release Assets

### GitHub Release Asset Bundle

Each tagged release must publish exactly these GitHub release assets:

- `xg2g_<version>_linux_amd64.tar.gz`
- `xg2g_<version>_linux_arm64.tar.gz`
- `xg2g_<version>_darwin_amd64.tar.gz`
- `xg2g_<version>_darwin_arm64.tar.gz`
- `xg2g_<version>_windows_amd64.tar.gz`
- `checksums.txt`

`<version>` means the tag version without the leading `v`.

### Archive Payload Contract

Every release archive must contain:

- one platform daemon binary: `xg2g` or `xg2g.exe`
- `README.md`
- `LICENSE`
- `backend/VERSION`
- `docs/**`

The verifier treats archive wrapper directories as implementation detail. The
required payload entries may be nested, but they must be present.

### Registry Publication Outputs

Each tagged release must publish exactly these registry-facing outputs:

- `ghcr.io/manugh/xg2g:vX.Y.Z-amd64`
- `ghcr.io/manugh/xg2g:vX.Y.Z-arm64`
- `ghcr.io/manugh/xg2g:vX.Y.Z`
- `ghcr.io/manugh/xg2g:latest`

The architecture-specific tags and the version manifest are normative release
outputs. `latest` is also part of the current release contract.

## Non-Contract Outputs / Explicit Exclusions

These files or classes are release governance inputs or build internals, but
they are not published release outputs:

- `RELEASE_MANIFEST.json`
- `DIGESTS.lock`
- GoReleaser `dist/` internals and temporary build contexts
- copied helper files such as `build-ffmpeg.sh`, `ffmpeg-wrapper.sh`,
  `ffprobe-wrapper.sh`
- SBOM, signatures, provenance, or attestations

Those outputs may exist in CI or future release flows, but they are not part of
the current external release guarantee unless this contract is updated.

## Truth Inputs

The release output contract is derived from:

- `.github/workflows/release.yml`
- `.goreleaser.yml`
- `infrastructure/docker/Dockerfile.release`
- `backend/VERSION`

## Verification

The contract entrypoint is
`backend/scripts/verify-release-output-contract.sh`.

Verification has two modes:

1. PR/CI governance mode:
   validates release workflow/config semantics and runs synthetic positive and
   negative bundle tests.
2. Bundle audit mode:
   `backend/scripts/verify-release-output-contract.sh --verify-bundle-dir <dir> --version <tag>`

Bundle audit mode verifies:

- exact asset filenames
- `checksums.txt` coverage over the archive set
- required payload entries inside each archive
- rejection of unexpected published output
