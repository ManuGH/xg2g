# Testing Strategy & Governance

This document defines the testing standards, scope, and governance for the xg2g project.
Every release must explicitly state its test coverage against this matrix.

## 1. Test Matrix (Dauerhafte Wahrheit)

| Component | Test Type | Automated (CI) | Manual | Release Blocking | Notes |
| :--- | :--- | :---: | :---: | :---: | :--- |
| **Auth** | Unit | ✅ | ❌ | ✅ | Strict token validation |
| **Config** | Unit | ✅ | ❌ | ✅ | Strict mode enforcement |
| **API** | Unit/Integration | ✅ | ❌ | ✅ | |
| **UI Embed** | CI Check | ✅ | ❌ | ✅ | `dist/**` check; Assumes Vite layout |
| **UI Rendering** | E2E/Manual | ❌ | ✅ | ⚠️ | Check `/ui` loading |
| **Rust Transcoder** | Unit/Integration | ✅ | ✅ | ⚠️ | CPU-based verification |
| **Docker Build** | CI (Buildx) | ✅ | ❌ | ✅ | Includes Trivy scan |
| **Docker Runtime** | Smoke Test | ❌ | ✅ | ⚠️ | Boot check |

## 2. Confidence Levels

Releases must declare confidence levels for major artifacts:

- 🟢 **High Confidence**: Fully covered by CI + Manual Verification.
- 🟡 **Medium Confidence**: Automated tests passed, limited manual scope.
- 🔴 **Low Confidence**: Not explicitly tested (use at own risk).

## 3. CI Architecture & Frequency

 | Workflow | Role | Frequency | Scope |
 | :--- | :--- | :--- | :--- |
 | `ci.yml` | **PR Gate** | On PR/Push | Fast feedback, `nogpu` contract, Lint, Smoke Test. |
 | `ci-v2.yml` | **Nightly/Extended** | Daily (02:17 UTC) | Deep regression (Rust FFI), Reproducible Builds. Manual monitoring required. |
 | `release.yml` | **Production Release** | Tag (`v*`) | Builds official artifacts (Binary + Docker), Signs, Publishes. |

 **Operational Note:** Nightly failures do not block PRs but must be investigated daily by maintainers. All schedules are UTC.

## 4. Test Scope Template (Per Release)

Copy this into the Release Pull Request or Walkthrough:

```markdown
### Test Scope (<Version>)

**Automated Tested**
- [ ] Unit: internal/auth, internal/config, internal/api
- [ ] CI: GoReleaser dry-run, Docker build (amd64)
- [ ] Security: CodeQL, Trivy

**Manually Tested**
- [ ] UI Access (/ui)
- [ ] Auth Flows
- [ ] Configuration (Strict Mode)

**Explicitly NOT Tested**
- [ ] Docker ARM64 (if applicable)
- [ ] Specific hardware/firmware versions
- [ ] Migration from deprecated versions
```

## 5. Release Approval Criteria

A release is approved **ONLY** if:

1. All **Release Blocking** tests in the Matrix pass.
2. Any **Low Confidence** areas are explicitly documented in Release Notes.
3. No "silent failures" or assumed coverage.
