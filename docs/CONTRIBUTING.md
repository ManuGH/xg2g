# Contributing to xg2g

Thank you for contributing to `xg2g`! To maintain system reliability, security, and long-term maintainability, all contributions must adhere to our engineering standards and governance process.

---

## 1. Engineering Governance & Invariants

Before starting work on any feature, fix, or refactoring, please read the [xg2g Engineering Charter](ENGINEERING_CHARTER.md).

All pull requests and architecture changes are governed by the **8 Core Invariants** defined in the Charter:
1. **Recording Protection**
2. **Lease Safety**
3. **Reader Protection**
4. **Playlist Consistency**
5. **Recovery Safety**
6. **Decision Explainability**
7. **Bounded Failure**
8. **No Partial Resource Leaks**

---

## 2. Architecture Decision Records (ADRs)

Architectural design decisions that operationalize or modify system principles are documented as **Architecture Decision Records (ADRs)** in `docs/ADR/`.

* **When an ADR is required:** Changes to the Engineering Charter, modifications to existing invariants, or introducing major component patterns (e.g. state machines, storage models, lease arbitration).
* **When an ADR is NOT required:** Bug fixes, refactorings, or new features that implement existing invariants without altering system principles.

To propose a new ADR, submit a PR adding a new file in `docs/ADR/` following the existing template and index.

---

## 3. Pull Request Review & Merge Gate

Every Pull Request that impacts system behavior or an invariant must provide the following **Required Engineering Evidence** in the PR description:

| Required Engineering Evidence | Expectation |
| :--- | :--- |
| **1. Invariant Impact** | Which existing invariant(s) does this change rely on, preserve, or affect? |
| **2. Failure Analysis** | What new failure modes, race conditions, or operational risks are introduced? |
| **3. Recovery & Compensation** | How is a failure detected, compensated, or reconciled? |
| **4. Durable Verification** | What automated mechanism (Unit, Integration, Contract, Race Test, CI Gate) permanently guards against regressions? |

---

## 4. Language Policy

* All source code, identifiers, APIs, database schemas, configuration keys, and commit messages **must be written in English**.
* All ADRs, architecture documents, code comments, developer documentation, and pull request descriptions **should be written in English**.
* Existing non-English code or documentation should be migrated to English opportunistically when touched by functional changes.

---

## 5. Development & Testing Workflow

* **Local Environment:** Development takes place in `/Users/manuel/StudioProjects/xg2g`.
* **Testing:** Run tests prior to opening a PR:
  ```bash
  go test ./...
  ```
* **Deployment Testing:** Use `./scripts/fast_deploy.sh` to test staging builds on LXC 110.
