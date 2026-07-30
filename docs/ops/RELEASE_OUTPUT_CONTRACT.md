# Release Output Contract

Canonical publication contract for `xg2g` releases. This defines which release
artifacts are normatively guaranteed, which files are only release governance
inputs, and which outputs are explicitly outside the published contract.

## Policy

- Missing normative release output is a blocker.
- `unexpected published output` is a blocker.
- Release output changes require updating this document, the verifier, and the
  release configuration in the same slice.
- Release-tag registry outputs MUST be published by `.github/workflows/release.yml`
  only. Auxiliary workflows may publish supporting images, but they must not
  republish `ghcr.io/manugh/xg2g:vX.Y.Z*`.
- The canonical version source is `backend/VERSION` in tag form (`vX.Y.Z`).
- GitHub release archive names use GoReleaser `{{ .Version }}` semantics
  (`X.Y.Z` without the leading `v`).
- Registry publication uses tag semantics (`ghcr.io/manugh/xg2g:vX.Y.Z`).
- A release is assembled as a draft and published only after every normative
  file and registry output has been verified and attested.
- Published releases are immutable: their tag and assets cannot be replaced.

## Normative Published Release Assets

### GitHub Release Asset Bundle

Each tagged release must publish exactly these GitHub release assets:

- `xg2g_<version>_linux_amd64.tar.gz`
- `xg2g_<version>_linux_arm64.tar.gz`
- `xg2g_<version>_linux_amd64.tar.gz.spdx.json`
- `xg2g_<version>_linux_arm64.tar.gz.spdx.json`
- `checksums.txt`
- `checksums.txt.sigstore.json`

xg2g is a Linux server (Docker/systemd); macOS and Windows binaries are not
built or published.

`<version>` means the tag version without the leading `v`.

### Archive Payload Contract

Every release archive must contain:

- the daemon binary: `xg2g`
- `README.md`
- `LICENSE`
- `backend/VERSION`
- `DIGESTS.lock`
- `docs/**`
- the `docs/man/xg2g.1` manual page
- `infra/systemd/**`
- the deployment helpers required by `infra/systemd/sync.sh` under
  `backend/scripts/`

The archive is a self-contained installation bundle. Running
`infra/systemd/setup-linux.sh` from an official release archive must not clone
the repository or resolve a mutable branch. GitHub-generated source ZIPs are
not release artifacts and must fail with a link to the matching Releases page
instead of guessing a tag from `backend/VERSION`.

The verifier treats archive wrapper directories as implementation detail. The
required payload entries may be nested, but they must be present.

### Registry Publication Outputs

Each tagged release must publish exactly these registry-facing outputs:

- `ghcr.io/manugh/xg2g:vX.Y.Z`
- `ghcr.io/manugh/xg2g:latest`

The version tag and `latest` are published by GoReleaser `dockers_v2` as single
multi-architecture manifests (`linux/amd64` + `linux/arm64`); there are no
per-architecture suffix tags. Both manifests are normative release outputs.
The manifest carries a BuildKit SBOM/provenance referrer, is signed keylessly
with Sigstore, and receives a GitHub build-provenance attestation.

### Provenance and Publication Gate

- `checksums.txt.sigstore.json` is the keyless Sigstore bundle for
  `checksums.txt`.
- Each archive has a matching SPDX JSON SBOM.
- GitHub artifact attestations bind every archive, SBOM, checksum file and
  Sigstore bundle to the tagged workflow run.
- GitHub's immutable-release attestation binds the published tag, source commit
  and final release assets.
- The release workflow verifies the remote OCI digest and both required
  platforms while the GitHub release is still a draft.

## Non-Contract Outputs / Explicit Exclusions

These files or classes are release governance inputs or build internals, but
they are not published release outputs:

- `RELEASE_MANIFEST.json`
- `DIGESTS.lock`
- `ghcr.io/manugh/xg2g-ffmpeg:<ffmpeg-version>` reusable FFmpeg base image
- GoReleaser `dist/` internals and temporary build contexts
- copied helper files such as `build-ffmpeg.sh`, `ffmpeg-wrapper.sh`,
  `ffprobe-wrapper.sh`

Native `.deb`, `.rpm`, Flatpak, AppImage and Snap packages are also excluded.
xg2g's supported lifecycle remains the self-contained archive installer and
the OCI/systemd deployment path; adding a package format requires tested
install, upgrade, removal and service-lifecycle contracts.

## Truth Inputs

The release output contract is derived from:

- `.github/workflows/release.yml`
- `.github/workflows/ffmpeg-base.yml`
- `.goreleaser.yml`
- `infra/docker/Dockerfile.ffmpeg-base`
- `infra/docker/Dockerfile.release`
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
- `checksums.txt` coverage over the archives and their SBOMs
- required payload entries inside each archive
- one SPDX SBOM per archive
- one Sigstore bundle for `checksums.txt`
- rejection of unexpected published output
