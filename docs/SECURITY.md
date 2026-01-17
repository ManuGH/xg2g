# Security & Trust Boundaries

## Scanner Signal Governance

To maintain a "100% Signal" security gate, the scanner configuration is
optimized to eliminate false positives in immutable, machine-generated
artifacts.

### 1. Generated Code Trust (exclude-generated)

Machine-generated files are excluded from static analysis (`gosec`) for the
following reasons:

- **Noise Reduction**: Generators frequently produce patterns (e.g., Auth Scopes
  named after credentials) that trigger false positives like
  `G101 (Potential hardcoded credentials)`.
- **Immutability**: These files are not intended for manual modification. Any
  "patch" applied to a generated file will be overwritten by the next build.
- **Upstream Trust**: Security is enforced at the **Source of Truth** (the
  OpenAPI specification) and the **Generator version**.

#### Excluded Patterns

- `**/server_gen.go`
- `**/types_gen.go`
- `**/client_gen.go` (if applicable)

### 2. Supply-Chain & Integrity Controls

The exclusion of generated code from runtime scans is compensated by **strong
mechanical provenance** at build time:

#### A. Hermetic Tool Pinning

All code generators (e.g., `oapi-codegen`) are strictly version-pinned in
`internal/tools/tools.go` and vendored in `vendor/`. This prevents "silent
drift" or un-audited updates to the generator logic.
> [!NOTE]
> See [HERMETICITY.md](file:///root/xg2g/docs/HERMETICITY.md) for the mechanical
> implementation of this guarantee.

#### B. The Code-Gen Contract

CI enforces that the committed generated code is perfectly in sync with the
current OpenAPI specification.

- **Enforcement**: `make verify-generate` or `git diff --exit-code` after running
  `make generate`.
- **Logic**: If the generator produced insecure code, the fix must be applied to
  the **OpenAPI Spec** or the **Pinned Generator Version**, never the output.

### 3. Policy for #nosec Annotations

Manual `#nosec` annotations are permitted **only in hand-written code** and
must be accompanied by a justification comment.

1. **Identify**: pinpoint the exact rule (e.g., `G101`).
2. **Justify**: Explain why it is a false positive or a managed risk.
3. **Audit**: These annotations are subject to review by security-aware peers.

---

**Policy Status**: CTO-Mandated 2026
**Compliance**: High Signal, Zero Tolerance for Un-audited Exceptions.
