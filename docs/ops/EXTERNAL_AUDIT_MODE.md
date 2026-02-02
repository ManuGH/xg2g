# External Audit Mode (ZIP Review + Offline Verification)

This repository supports external/offline audits via a source-only ZIP snapshot.
Goal: enable third-party review (including sandboxed tooling) without requiring repo access or internet.

## 1) Artifact: Source-only ZIP
### How to produce
Preferred (if present):
- `make review-zip`

Otherwise:
- `git archive --format=zip --prefix=xg2g-main/ HEAD -o xg2g-main.zip`

### Purity requirements
The ZIP must be source-only:
- no `dist/` outputs committed
- no large binaries or media in root
- generated artifacts must be reproducible from repo truth

---

## 2) Audit Inputs
Auditor receives:
- `xg2g-main.zip`
- optional: `DIGESTS.lock`, `RELEASE_MANIFEST.json` (if release context is being audited)
- optional: a short “Audit Context” note (what to verify)

---

## 3) Offline Verification (auditor checklist)
Offline means no internet access (no go get, no npm install, no remote fetch).
make ci-pr is Go-only and does not require Node.

### A) Integrity
- Unzip and confirm tree structure is intact
- Confirm no obviously generated/compiled artifacts were included unexpectedly

### B) Deterministic gates (offline safe)
Run:
- `make ci-pr`

Expected:
- builds/tests succeed under the pinned toolchain policy
- no drift / no uncommitted generation required

### C) Optional deep verification (if time)
Run:
- `make verify`
- `make ci-nightly` (only if environment supports heavier tooling)

---

## 4) Evidence package (what counts as proof)
Auditor should record:
- tool versions:
  - `go version`
  - `go env | grep -E '(GOTOOLCHAIN|GOFLAGS|GOVCS|GOPROXY|GOSUMDB)'`
- command transcript:
  - `make ci-pr` output
- deterministic state:
  - `git diff --exit-code` (in a git checkout), or
  - file hashes of key generated artifacts (if applicable)

---

## 5) What Audit Mode is NOT
- Not a replacement for GitHub PR checks
- Not a release attestation system by itself
- Not an excuse to commit build outputs to “help reviewers”

---

## 6) Recommended audit flow (minimal)
1. Produce ZIP from a clean `main` HEAD.
2. Auditor runs `make ci-pr` offline.
3. Auditor reports:
   - PASS/FAIL
   - tool versions
   - any diffs or invariant violations
