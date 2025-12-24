# Release Checklist

Use this checklist for every release to ensure governance compliance.

## Pre-Release

- [ ] **Test Scope Defined**: `docs/TESTING.md` matrix consulted.
- [ ] **Automated Tests**: CI (Build, Lint, Test, Scan) is Green.
- [ ] **Manual Verification**: Performed for UI/Runtime components.
- [ ] **Exclusions**: Known untested areas documented.

## Release Action

- [ ] **Version Bump**: `package.json`, `config`, etc. updated.
- [ ] **Changelog**: Updated with strict segregation (Features/Fixes) and Confidence Levels.
- [ ] **Tag**: Created immutable tag (e.g., `v3.0.1`).

## Release Approval

- [ ] All mandatory tests passed.
- [ ] Deviations documented.
- [ ] Signed off by Maintainer.
