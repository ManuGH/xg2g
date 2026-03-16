# Release Template

Use this when you need curated GitHub release notes instead of commit-only
automation.

## One-line highlight

`xg2g vX.Y.Z improves browser-ready Enigma2 streaming with [primary outcome].`

## Why this release matters

One short paragraph for operators and evaluators. Focus on user-visible
outcomes, deployment impact, or a specific reliability/security improvement.

## What changed

### Streaming and Playback

- Describe playback, transcoding, compatibility, or delivery-policy changes.
- Mention Safari, iPhone, iPad, Chrome, or recording behavior if relevant.

### Security

- Describe auth, token, secret, hardening, or fail-closed behavior changes.

### Web UI and API

- Call out user-facing UI changes and integration-relevant API updates.

### Operations and Infra

- Note deployment, Docker, systemd, CI, observability, or release-pipeline changes.

### Documentation

- Mention new guides, migration notes, architecture docs, or runbooks.

## Upgrade notes

- State whether action is required.
- Include config or env var changes explicitly.
- Link to deployment or configuration docs when needed.

## Breaking changes

- `None` if there are none.
- Otherwise list each breaking change as a short, concrete operator action.

## Quick links

- Docker image: `ghcr.io/manugh/xg2g:vX.Y.Z`
- Quickstart: <https://github.com/ManuGH/xg2g?tab=readme-ov-file#quickstart>
- API reference: <https://manugh.github.io/xg2g/>
- Deployment guide: <https://github.com/ManuGH/xg2g/blob/main/docs/ops/DEPLOYMENT.md>
- Security policy: <https://github.com/ManuGH/xg2g/blob/main/SECURITY.md>

## Safari/iOS Manual Repro (Gate Z)

- [ ] iOS Safari Live playback (A) - PASS (device/OS: ___)
- [ ] iOS Safari VOD playback + resume/seek (B) - PASS (device/OS: ___)
- [ ] Fail-closed negative case (C) - PASS
- [ ] macOS Safari spot check (D) - PASS (OS/Safari: ___)

## Evidence

- requestId(s):
- screenshots / screen recording:
- known limitations or waiver reason (if any):
