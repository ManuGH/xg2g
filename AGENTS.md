# AGENTS.md

## Ops Triage Truth

For `xg2g` start/restart incidents, do not assume checked-in docs match the live host.
Capture and compare these three files before patching anything:

- `/etc/systemd/system/xg2g.service` — installed unit that systemd actually runs
- `/srv/xg2g/docker-compose.yml` — frozen base Compose source of truth for the `xg2g` service image
- `/srv/xg2g/docker-compose.gpu.yml` — optional GPU overlay; compare it too when present
- `/etc/xg2g/xg2g.env` — live environment file loaded by both systemd and Compose; may also select compose files via `COMPOSE_FILE`

The checked-in canonical unit is [deploy/xg2g.service](deploy/xg2g.service), rendered from [backend/templates/docs/ops/xg2g.service.tmpl](backend/templates/docs/ops/xg2g.service.tmpl). The live unit may drift from both the repo truth and the deployed host copy under `/srv/xg2g/docs/ops/xg2g.service`.

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

**Updated 2026-07-22 (post-migration to `pve2`):** OpenClaw was never adopted
in production. The `/root/xg2g` read-only mirror and `/root/xg2g-build`
detached build checkout described in older revisions of this section did not
reflect a live process and have been retired — do not recreate them.

- GitHub is the canonical source for committed code.
- The Mac `StudioProjects` checkout is where Manuel develops and reviews.
  Never a build or deployment source.
- The Proxmox hypervisor (`pve2`, see infra docs) has no build role and no
  `xg2g` checkout. It is VM/LXC management plane only.
- **LXC 110 `/srv/xg2g` and `/srv/xg2g-staging` are the authoring AND build
  checkouts, in addition to being the runtime surfaces.** `git pull` from
  `origin` happens directly there; `make build-with-ui` runs in-container;
  the resulting binary is copied straight onto the bind-mounted path used by
  the corresponding Docker container — no cross-host binary transfer step.
  Staging is verified on `:8089`, production on `:8088`.

A clean GitHub commit may be propagated one-way into LXC 110's checkouts; no
tool may silently synchronize uncommitted files between hosts.
