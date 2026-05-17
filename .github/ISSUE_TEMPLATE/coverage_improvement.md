---
name: "Coverage Improvement"
about: "Increase targeted test coverage for a component"
labels: ["coverage-improvement", "testing"]
---

> **Note**: This project follows an English-only policy.
> Please write your issue in English to ensure it can be understood by all
> contributors.

## Component

- [ ] api
- [ ] proxy
- [ ] daemon
- [ ] epg
- [ ] playlist
- [ ] owi

## Current / Target Coverage

- **Current:** <!-- e.g. 64.4% -->
- **Target (PR):** <!-- e.g. +3..8 pp, or API ≥70% -->

## Approach (check all that apply)

- [ ] Extract interfaces / invert dependencies
- [ ] Table-driven tests for handlers
- [ ] Circuit-breaker state tests
- [ ] Fake FFmpeg / fake prober
- [ ] Error paths (timeout, non-zero exit, empty streams)
- [ ] Contract test against mock OpenWebIF (CI-only)
- [ ] I/O abstraction (`io.Reader` / `io.Writer`)
- [ ] Chaos-style tests (latency, upstream failure)

## Test Flags

- [ ] unit tests
- [ ] integration
- [ ] contract

## Acceptance Criteria

- [ ] Patch coverage ≥90%
- [ ] Component coverage +X pp
- [ ] Flaky rate <5%
- [ ] CI time not increased (or justified)
- [ ] Tests are deterministic (no sleeps)

## References

- [Coverage workflow](../workflows/coverage.yml)
- [CI Failure Playbook](../../docs/ops/CI_FAILURE_PLAYBOOK.md)

## Implementation Plan

<!-- Describe the concrete steps: -->
1.
2.
3.

## Definition of Done

- [ ] Tests written and passing locally
- [ ] Coverage target reached (locally with `go tool cover`)
- [ ] CI passing (all checks green)
- [ ] Codecov patch coverage ≥90%
- [ ] No flaky tests (3x locally without failure)
- [ ] Code review approved
