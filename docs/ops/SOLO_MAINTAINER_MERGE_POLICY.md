# Solo Maintainer Merge Policy

Purpose: keep merges unblocked for a single maintainer while preserving hard safety gates and auditability.

## Effective Date

- 2026-02-15

## Branch Protection Baseline (`main`)

Required checks stay mandatory. Solo flow removes only human-review deadlocks.

- `required_status_checks`: enabled (strict)
- `required_approving_review_count`: `0`
- `require_code_owner_reviews`: `false`
- `required_conversation_resolution`: `false`
- `enforce_admins`: `true`
- `required_linear_history`: `true`

## CODEOWNERS Mode

- No global `*` fallback in solo mode.
- CODEOWNERS entries exist only for critical paths.
- This keeps ownership intent without forcing impossible self-review.

## Normal Solo Merge Flow

1. Open PR from topic branch to `main`.
2. Required checks must all be green.
3. Resolve or acknowledge review threads.
4. Merge normally (or admin merge if ruleset requires it), with squash preferred.

## Admin Override (Allowed, Not Emergency)

Admin merge is allowed in solo mode when all required checks are green and no policy gate is bypassed.

Mandatory PR comment before merge:

- why override is needed
- confirmation that required checks are green
- scope statement (changed files/surface)

## Ruleset Checklist (when editing GitHub settings)

- [ ] Required checks unchanged unless explicitly approved.
- [ ] `required_approving_review_count` for `main` remains `0` in solo mode.
- [ ] `require_code_owner_reviews` remains `false` in solo mode.
- [ ] `required_conversation_resolution` remains `false` in solo mode.
- [ ] `enforce_admins` remains `true`.
- [ ] Any change is documented in PR/issue with timestamp.

