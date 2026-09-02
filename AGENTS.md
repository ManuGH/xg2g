# AGENTS.md

## Infrastructure & Network Architecture Truth

Always use the following verified host & network topology (do not use obsolete IPs):

- **Proxmox VE Host (`pve2`):** `10.10.55.182`
  - SSH: `ssh proxmox` (or `ssh pve2`) using `~/.ssh/id_ed25519_mac_20260722`.
  - **CRITICAL:** `10.10.55.2` has been deleted and must NEVER be addressed.
- **LXC 110 (`xg2g-dev` / `10.10.55.14`):**
  - **Staging:** Port `:8089` (`xg2g-staging` container in `/srv/xg2g-staging/`).
  - **Production:** Port `:8088` (`xg2g.service` in `/srv/xg2g/`).
  - **Build Dir:** `/srv/xg2g-build`.
  - **Fast-Deploy:** `./scripts/fast_deploy.sh --confirm-staging`.
- **LXC 132 (`caddy` / `10.10.55.12`):**
  - Central reverse proxy with TLS wildcard cert `*.home.matrixcentral.de`.
  - Routes `xg2g.home.matrixcentral.de` $\rightarrow$ `10.10.55.14:8089` (Staging).
- **VM 100 (`OPNsense` / `10.10.55.254`):**
  - Gateway, router, and WireGuard VPN server.
- **Enigma2 Receiver (`10.10.55.64`):**
  - VU+ Uno 4K receiver source (`root:rK8pN4sV6mQ2xT9`).

## Ops Triage Truth

For `xg2g` start/restart incidents, do not assume checked-in docs match the live host.
Capture and compare these three files before patching anything:

- `/etc/systemd/system/xg2g.service` — installed unit that systemd actually runs
- `/srv/xg2g/docker-compose.yml` — frozen base Compose source of truth for the `xg2g` service image
- `/srv/xg2g/docker-compose.gpu.yml` — optional GPU overlay; compare it too when present
- `/etc/xg2g/xg2g.env` — live environment file loaded by both systemd and Compose; may also select compose files via `COMPOSE_FILE`

The checked-in canonical unit is [infra/systemd/xg2g.service](infra/systemd/xg2g.service), rendered from [backend/templates/docs/ops/xg2g.service.tmpl](backend/templates/docs/ops/xg2g.service.tmpl). The live unit may drift from both the repo truth and the deployed host copy under `/srv/xg2g/docs/ops/xg2g.service`.

## Env Reload Rule (LXC 110)

`docker compose restart` does NOT reload changed env files — containers keep
the environment they were created with. After editing `/etc/xg2g/xg2g.env` or
`/etc/xg2g/xg2g-staging.env`, always run
`docker compose up -d --force-recreate` in the corresponding compose directory
and verify the running container actually sees the new value
(`docker exec <container> printenv <VAR>`), never trust the file alone.
Confirmed incident 2026-07-20: staging kept running with a rotated-away
signing key after a plain `restart`.

## Restart Failure Order

Run these first:

```bash
systemctl status xg2g.service --no-pager -l
journalctl -xeu xg2g.service --no-pager -n 120
docker inspect -f '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{else}}no-health{{end}}' xg2g
docker logs --since 5m xg2g
```

Then classify the failure before editing:

- `ExecStartPre` fails with `No such image`: the live unit is likely checking a stale hardcoded tag. Treat `services.xg2g.image` in `/srv/xg2g/docker-compose.yml` as image truth, not an old registry tag.
- Container logs fail with `XG2G_DECISION_SECRET is required but not set`: `/etc/xg2g/xg2g.env` is missing a mandatory live-stream signing secret. Required length is at least 32 ASCII bytes; see [docs/ops/SECURITY.md](docs/ops/SECURITY.md).
- `systemctl start` or `restart` fails at `ExecStartPost` with `Container is unhealthy`: inspect Docker health details, not just `/readyz`.

## Health Nuance

`/readyz` can return `200` while Docker health is still `unhealthy`.
When that happens, inspect the container health log directly:

```bash
docker inspect -f '{{json .State.Health}}' xg2g
```

One confirmed failure mode is metrics-only health drift:

- readiness endpoint is healthy
- Docker healthcheck still fails because `http://localhost:9091/metrics` is unreachable

That symptom means the service is running far enough to answer readiness, but systemd will still fail the start because Docker health never turns green.

## Documentation Rule

If the repo template, the checked-in runbook, and the live host disagree, update [docs/ops/RUNBOOK_SYSTEMD_COMPOSE.md](docs/ops/RUNBOOK_SYSTEMD_COMPOSE.md) with the exact observed delta before doing larger cleanup work.

## Collaboration Contract

This repository is worked on by Codex, Gemini/Antigravity, Claude Code,
OpenClaw/DeepSeek, and GitHub review automation. The rules below define
ownership; an agent's available tools do not grant it permission to use them.

### Roles and authority

- Gemini Code Assist is a reviewer. Its findings are evidence to evaluate,
  not instructions to apply blindly.
- OpenClaw is the default read-only monitor. It may inspect checks, cache
  context, and report blockers, but must not edit, commit, push, comment,
  label, mark a PR ready, resolve a thread, merge, or deploy by default.
- Codex is the primary GitHub integration owner. Codex classifies review findings,
  coordinates delegated fixes, writes the authoritative replies, resolves
  threads only after verification, and prepares the canonical integration PR.
- Antigravity and Claude Code normally implement only explicitly delegated,
  bounded tasks in their own branch and worktree, handing code and evidence
  back to Codex.
- **Dynamic Fallback (Token Exhaustion):** If Codex is unavailable (e.g., due to
  token limits), Antigravity or Claude Code may dynamically assume the role of
  the integration owner. In this mode, they are authorized to handle tasks
  end-to-end: writing code, testing via fast-deploy, committing, pushing 
  branches, and preparing PRs, handing over directly to Manuel.
- Manuel is the final authority for merging and every production promotion.

### Review-comment lifecycle

For every review comment, use this sequence:

1. Read the complete thread and current diff.
2. Classify the finding as valid, stale, duplicate, intentional, or blocked.
3. If valid, implement the smallest fix in an isolated worktree.
4. Run the relevant tests and record the result.
5. Codex replies with the evidence and resolves the thread only after the fix
   is present on the PR head.

Outdated comments are not silently treated as fixed. They are either answered
with the commit that superseded them or explicitly documented as obsolete.

This lifecycle applies to every agent and every mode, including the Dynamic
Fallback role. Resolving a thread via API (`resolveReviewThread` mutation or
otherwise) without a fix commit on the PR head or a written reply in the
thread is prohibited. Bot reviewers (e.g. gemini-code-assist) count as
reviewers: their findings get a fix or a one-sentence justification in the
thread before the thread is resolved — never a silent resolve.

### Merge policy

- Admin merge (`gh pr merge --admin`) may bypass the review-approval gate —
  this is accepted solo-repo reality — but it must NEVER bypass CI. Admin
  merge is allowed only after all required checks have completed green;
  merging over pending or failing checks is prohibited.
- Before any merge, confirm there are no unresolved review threads that lack
  a fix or a written reply (see lifecycle above).
- Delegated merges (decided by Manuel, 2026-07-20): agents may merge a PR —
  prefer `gh pr merge --auto` so branch protection stays the enforcer — once
  every required check is green and every review thread is fixed or answered.
  Manuel remains the escalation point and can revoke this delegation at any
  time. Production promotion is never delegated.

### Release and tag safety

A GitHub release is a separate, auditable publication after merge. It is not a
deployment and never authorizes staging or production promotion. Follow
[docs/ops/RELEASE_OUTPUT_CONTRACT.md](docs/ops/RELEASE_OUTPUT_CONTRACT.md) as
the normative output contract.

1. Before selecting a SemVer or editing release metadata, complete a
   release-readiness audit. Inventory changes to the release workflow,
   packaging, entrypoint, runtime image, installer, updater, uninstaller,
   signatures, attestations, and required toolchain versions. Record one
   coherent release contract before preparing the release.
2. Land separable release-infrastructure changes through their own reviewed PR
   and verify them on `main` before opening the release-preparation PR.
   Never use public tags or patch versions as release-pipeline experiments.
   Validate the publisher with local, snapshot, dry-run, and untagged PR/main
   checks first.
3. Prepare every release from a clean, isolated worktree based on the latest
   `origin/main`. Never release from a feature branch or dirty checkout.
4. Create and commit
   `docs/release/vX.Y.Z_behavioral_changes.txt` first, even when it only states
   that runtime, API, configuration, and deployment behavior are unchanged.
   Then run `backend/scripts/release-prepare.sh vX.Y.Z` from the clean tree,
   review every generated change, and commit the preparation coherently.
5. Put release preparation through a PR. Run `make ci-pr` and `make pre-push`;
   merge only after all GitHub checks are green and no review thread lacks a
   fix or written disposition.
6. Before tagging, fetch `origin/main`, confirm `backend/VERSION` and
   `RELEASE_MANIFEST.json` match the intended tag, confirm the tag does not
   exist, and confirm immutable releases are enabled. Create an annotated tag
   on the exact merged commit and push only that tag.
7. `.github/workflows/release.yml` is the only release publisher. It must keep
   the release as a draft until archive/SBOM generation, checksum and OCI
   signing, remote multi-platform verification, and GitHub attestations have
   all succeeded. Never bypass a failed workflow with a manual
   `gh release create`, public draft edit, replacement asset, or moved tag.
8. Treat every pushed tag as permanent. If a tagged run fails, retain the tag
   and failed run as audit evidence, remove an incomplete unpublished draft
   only after recording the failure, fix the cause through a new reviewed PR,
   verify the root cause through the untagged pipeline, and select a new SemVer
   (normally the next patch). Never delete, reuse, or force-move a failed or
   published release tag, and never repeat the same unverified correction as a
   patch-release cascade.
9. Do not report a release complete until the public release is `latest`,
   non-draft, non-prerelease, and immutable; its exact asset bundle downloads
   and passes checksums; both SPDX SBOMs parse; file and OCI attestations verify;
   the version tag and `latest` resolve to the same OCI digest with
   `linux/amd64` and `linux/arm64`; and the Sigstore checksum and OCI signatures
   verify against the tagged release workflow identity.
10. Treat a successful stable release as a terminal state. Do not create
    another version merely to continue cleanup or retry release machinery.
    Require a concrete corrected artifact, runtime, user-visible, or security
    reason plus explicit operator intent.

If any verification fails, stop publication or leave it failed closed and
report the exact run, tag, draft state, and next corrective action. A partially
successful release is not a successful release.

### Branch and worktree rules

- Inspect `git status`, branch, worktrees, and remote tracking state before
  editing.
- Never reset, clean, switch, or delete a dirty checkout to make it convenient
  for an agent. Preserve existing user changes and ask for a decision when
  ownership is unclear.
- Every delegated implementation uses one named branch and one isolated
  worktree. Do not create timestamp worktrees on repeated retries.
- Keep generated frontend bundles separate from source changes; never delete
  or regenerate them without stating why and verifying the resulting diff.
- Commits should be small, coherent, and named by intent. Do not mix a
  reviewer fix, unrelated UI work, deployment changes, and generated assets in
  one commit.
- One branch is one subject. If the working tree holds changes that would need
  more than one commit subject, they belong on separate branches. Do not park
  unrelated work on whichever branch happens to be checked out — a CI change
  and an RBAC fix sitting on `fix/onboarding-papercuts` are two lost branches,
  not one.

#### Branching base (added 2026-08-31 after the third occurrence)

- Cut every branch from a freshly fetched `origin/main`, never from another
  feature branch. A branch cut from a branch carries the parent's commits as
  its own: it reports hundreds of commits ahead, its PR is unreviewable, and
  the two can no longer merge independently.
- Before the first push, run `git rev-list --count origin/main..HEAD`. If that
  number is larger than the work actually done, the base is wrong — rebase onto
  `origin/main` now, not after review starts.
- The same check exposes duplicates. If `git rev-list --count B..A` is `0`,
  branch A is fully contained in B and one of the two is redundant.

#### Closing a branch out

- A branch that outlives its session either has an open PR or a written note
  saying what blocks it. A branch with no PR and no note is abandoned work that
  drifts further from `main` every day and will not be mergeable later.
- Delete the branch and remove its worktree as soon as its PR merges. Rebase
  and squash merges leave the local branch reporting commits "ahead" of `main`
  even though nothing is outstanding; confirm with
  `git cherry origin/main <branch>` — every line prefixed `-` means every commit
  is already in `main` and the branch is only a stale pointer.
- A `+` from `git cherry` is not proof that work is outstanding. It compares
  patch IDs, and any commit whose conflicts were resolved on the way in has a
  different patch ID from its original — so content that IS in `main` reports as
  missing. Seen 2026-09-02: a branch reported 2 of 34 commits outstanding, and
  both were exactly the commits whose conflicts had been resolved during the
  split. Before acting on a `+`, grep `main` for what that commit actually
  added. Count content, not patch IDs.
- Before removing a worktree, run `git status --porcelain` inside it. Agent
  worktrees routinely hold uncommitted test files. Commit them or save a patch
  first. Never `git worktree remove --force` a dirty tree merely to tidy up.
- Periodically reconcile: `git branch -vv`, `git worktree list`, and
  `gh pr list --state all` together. Anything local without a matching open PR
  is either finished (delete it), parked (note it), or forgotten (decide).

#### Before merging a branch that has fallen behind

- Ask first whether its work already landed by another route. A branch whose
  headline work reached `main` independently is not merged forward, it is
  closed. Compare tips, not the three-dot diff:
  `git diff --shortstat origin/main <branch>`. Deletions far exceeding
  insertions mean taking that branch would REVERT `main`. Seen 2026-09-02:
  +8,505/−87,921 on one branch, and on another `main` had implemented the very
  feature the branch still refused with an error.
- `git diff --numstat origin/main <branch> -- <file>` per key file says which
  side is ahead. A file that is byte-identical, or where the branch has fewer
  lines, is a file the branch can no longer contribute.
- Branch protection here is `strict: false`, so a PR may merge while far behind
  `main` and its CI never builds the combination. Either rebase before merging
  so CI tests what will actually land, or build and test merged `main`
  afterwards. Do not assume a green PR proves the merge result.

#### Linters do not agree across machines

- CI pins its linter version. A local `golangci-lint` of a different version
  reports different findings — seen 2026-09-02: local reported 0 issues where
  CI reported 3 G115s. A clean local run is not a green gate.
- When CI reports N findings of one pattern, grep the tree for the pattern
  instead of fixing the N reported lines. Same day: CI named 3 sites,
  `grep` found 6, and the other 3 would have failed a later run.
- `go vet` stops at the first errors per package and does not see everything a
  test binary will. `go test` is what proves a package with its tests compiles.

### Deployment and safety

- A commit is a checkpoint, not a completion, test result, release, or Manuel
  approval. A branch may contain work in progress.
- A push to a feature branch is a review handoff, not a deployment or release.
  Never push or open a PR for unfinished work unless the task explicitly calls
  for that handoff.
- Staging on LXC 110 requires an explicit operator action to start a test run.
  It is intentionally allowed before final review/merge and is never an
  approval of production readiness.
- Production promotion is a separate action and always requires Manuel's
  explicit approval after staging evidence is reviewed.
- The default deployment target is staging on `:8089`.
- Production on `:8088` requires explicit Manuel approval and a separate,
  auditable promotion step.
- A release promotion always stages the exact published OCI release first:
  `scripts/stage-release-candidate.sh --ref vX.Y.Z --confirm-staging`. Only
  that exact tag/commit may then be promoted with
  `scripts/promote_production.sh --ref vX.Y.Z --confirm-production`.
  Production uses the installed canonical `xg2g-admin update` path; copying a
  raw binary into production is prohibited.
- Before changing either live environment, capture the complete
  production/staging runtime evidence with
  `scripts/check-deployment-state.sh`: container image reference and immutable
  ID/digest, reported version/commit/build, binary hash, bind mounts, published
  addresses, Compose file chain, manifest mode, Docker health, and endpoint
  health. Do not infer runtime truth from a port, mutable tag, or label alone.
- Runtime lifecycle has exactly two valid steady states:
  `baseline` means `:8088` and `:8089` run the exact same immutable image,
  commit, and binary hash with no staging binary override; `candidate` means
  `:8089` runs a Git-descendant of production and its schema-v2 deploy
  manifest identifies that commit and hash. Staging may never
  remain older than production, diverge from production, or carry an untracked
  candidate. Run `scripts/check-deployment-state.sh` before and after every
  staging deployment or production promotion.
- Both maintainer runtime ports are loopback-only on LXC 110. `:8088` is
  reached through the governed production reverse proxy; `:8089` is reached
  only through an explicit SSH/VPN operator path and must never be published
  on `0.0.0.0` or unrestricted IPv6.
- After a candidate is promoted, `scripts/promote_production.sh` must restore
  staging to `baseline`. If an explicitly authorized out-of-band production
  update occurs, run
  `scripts/sync-staging-baseline.sh --confirm-staging-baseline` immediately
  after production verification. This pins staging to the exact running
  production image digest; it does not rebuild an artifact.
- These ports describe runtime roles, not repository permissions. Official
  installations use `:8088` on their own hosts; `:8089` is reserved for the
  maintainer staging instance and is not a second public product surface.
  GitHub write access, protected branches, required CI, and the release
  workflow control who may change official xg2g code and releases.
- Do not expose tokens, secrets, JWTs, or private host configuration in chat,
  logs, PR comments, or committed files.
- Do not use `git reset --hard`, broad cleanup, force-push, or destructive
  remote operations unless the operator explicitly requested that exact action.
- When live configuration differs from the repository, capture the live
  evidence first and document the delta before changing either side.

### Change contract

Before implementing a refactor, fix, feature, migration, or architectural
cleanup, write down a concise change contract. Small changes may use one line
per item; larger changes need explicit acceptance criteria. The contract must
state:

- **Fixed**: concrete incorrect behavior being corrected,
- **Improved**: existing behavior or structure being made better,
- **New**: new behavior, capability, abstraction, or public contract,
- **Removed**: code paths, flags, compatibility layers, or behavior deleted,
- **Unchanged**: behavior and interfaces that must deliberately remain stable,
- **Risks**: plausible regressions and affected boundaries,
- **Acceptance criteria**: observable evidence that proves completion,
- **Exit condition**: for migrations or parallel implementations, the exact
  condition and owner/action for removing the temporary path.

Do not describe a behavior change as a pure refactor. Use these categories
consistently:

- `fix`: corrects wrong behavior,
- `refactor`: changes structure without an intended behavior change,
- `feat`: introduces new behavior,
- `migration`: temporarily operates old and new paths,
- `cleanup`: removes a transition or code proven obsolete.

If work spans several categories, split it into coherent commits or document
the combined scope explicitly. During implementation, update the contract when
the actual scope changes instead of allowing silent scope drift.

At handoff, compare the result with the original contract and record:

- what was actually fixed, improved, introduced, and removed,
- deviations from the agreed scope and why they were necessary,
- acceptance criteria satisfied and the evidence for each,
- remaining temporary paths, debt, risks, and their next owner/action.

### Validation and handoff

Run `make pre-push` before every push (or install the hook once via
`make hooks-install`). A push that fails on gofmt, vet, or build wastes a
full CI round-trip — this happened repeatedly during the VOD cutover.
"Tests pass locally" is not a valid claim unless the exact CI target ran;
for the PR gate that is `make ci-pr`.

Every implementation handoff must state:

- branch and commit(s),
- files changed and files deliberately left untouched,
- tests or checks run and their result,
- deployment target (if any),
- unresolved review findings or known deviations,
- the exact next owner/action.

If a required external service, model provider, credential, or approval is
unavailable, stop the affected lane and report the blocker. Do not compensate
by spawning another writer, switching providers silently, or creating another
worktree.

## Linux-first Repository Topology

`xg2g` is a Linux/Go/Docker application. The Mac checkout is a development
client, not the runtime host.

**Updated 2026-07-30 (verified against LXC 110):** OpenClaw was never adopted
in production. The `/root/xg2g` read-only mirror and `/root/xg2g-build`
detached build checkout described in older revisions of this section did not
reflect the live topology and have been retired — do not recreate them.

- GitHub is the canonical source for committed code.
- The Mac `StudioProjects` checkout is where Manuel develops and reviews.
  It may run local validation, but it is never a Linux runtime or deployment
  surface.
- The Proxmox hypervisor (`pve2`, see infra docs) has no build role and no
  `xg2g` checkout. It is VM/LXC management plane only.
- LXC 110 `/srv/xg2g-build` is the only Linux fast-iteration build checkout.
  It stays clean, detached at an exact pushed commit, and is updated only by
  the governed staging workflow.
- LXC 110 `/srv/xg2g-staging` is a deployment surface, not a Git checkout.
  Its binary and `deploy-manifest` are produced from `/srv/xg2g-build`, and
  staging is verified on `:8089`.
- LXC 110 `/srv/xg2g` is the production install/runtime surface defined by
  [docs/ops/INSTALLATION_CONTRACT.md](docs/ops/INSTALLATION_CONTRACT.md), not
  an authoring or build checkout. A legacy Git checkout found there is
  migration drift: capture it, do not pull/build/edit it, and reconcile it
  only through the canonical install/sync path after explicit production
  approval. Production is verified on `:8088`.

A clean, pushed GitHub commit may be propagated one-way into the isolated
`/srv/xg2g-build` checkout and then explicitly deployed to staging. No tool may
silently synchronize uncommitted files between hosts or build from a runtime
surface.
