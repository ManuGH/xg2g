# CI Failure Playbook (xg2g)

This playbook defines the only allowed troubleshooting path when the required PR checks fail.
Goal: restore green deterministically without weakening guardrails.

## Required Checks (Branch Protection)
- CI / PR Gate (Fast & Deterministic)
- Prevent Large Files
- Verify Test Assets in testdata/

Non-required workflows (security scans, UI contracts, etc.) are informational unless explicitly promoted.

Solo-maintainer merge behavior is defined in `SOLO_MAINTAINER_MERGE_POLICY.md`.

---

## 0. Triage: classify the failure
### A) Job never starts (Queued / Pending, no runner assigned)
**Signal:** no runner name, no steps executed.
**Interpretation:** GitHub scheduling/backlog, not repo breakage.

**Action:**
1. Do **not** change workflows/guards because of queueing.
2. Quick sanity checks (do not change guards):
   - Runner label is valid for the workflow (e.g., `ubuntu-latest`, `ubuntu-24.04`).
   - Repo → Settings → Actions → General: Actions enabled.
3. Prove locally:
   - `make ci-pr`
   - (optional) `make verify`
4. If local is green, attach proof to PR (command output / log snippet).

**Escalation (only if blocking releases):**
- Add a self-hosted runner **only** for release-critical windows.
- Keep branch protection unchanged.

### B) Repo Hygiene failed (Large files / Test assets location)
These are “repo invariants”. Fix immediately, do not bypass.

---

## 1) Fix: Prevent Large Files
### What this check enforces
No newly-changed file over the configured size threshold.

### Typical causes
- accidentally committed binaries, archives, media, recordings
- generated artifacts (dist/, coverage.html, large logs)

### Fix steps
1. Identify offending file(s) from CI log output.
2. Remove from git history for the PR (prefer amend/rebase):
   - If file should not be tracked: delete and add proper ignore rule (if applicable).
3. If the file is a legitimate asset:
   - move into `testdata/` or host externally
   - or Git LFS (only if truly required)

**Never:**
- increasing thresholds
- “temporary allowlist”
- committing generated binaries

---

## 2) Fix: Verify Test Assets in testdata/
### What this check enforces
Test media/assets must live under `testdata/` (not in repo root).

### Fix steps
1. Move the asset into `testdata/...` (choose a stable, descriptive folder).
2. Update code/tests to reference the new location.
3. Re-run locally: `make ci-pr`

---

## 3) Fix: CI / PR Gate (Fast & Deterministic)
### What this gate is
The PR Gate must run:
- offline-safe (no network dependency for Go build/tests beyond standard tool installs in CI)
- deterministic outputs (no drift on generated files unless intentionally changed)
- fail-closed toolchain governance (GOTOOLCHAIN policy)

### Always reproduce locally first
Run:
- `make ci-pr`

If it passes locally but fails in CI:
- Compare environment:
  - Go toolchain version (must match pinned toolchain policy)
  - vendor present and consistent
  - no uncommitted generated changes

### Common failure classes
#### A) Toolchain mismatch
- Ensure you are using the pinned Go toolchain.
- Verify with the repo’s toolchain check script (if present).
- Re-run `make ci-pr`.

#### B) Drift failures (generated files / docs)
- Run the generating target(s) and commit the result.
- Never “ignore” diffs.

#### C) Test failures
- Fix the bug. No workaround commits.
- If flaky: convert to deterministic (clock, randomness, concurrency).

---

## 4) Proof of Fix (required in PR description when CI was red)
Add a short “Proof” section:
- command(s) run
- result summary
- any relevant artifacts (hashes, diff links)

Example:

## Proof
- local: `make ci-pr` ✅
- go: `go version` => ...
- clean tree: `git status --porcelain` => empty ✅
- notes: (optional) ...

---

## 5) Governance: what is not allowed
- weakening branch protection because of runner queues
- converting required checks into optional without an explicit CTO decision
- adding new required checks without a written rationale (risk, cost, determinism)
