# Security Policy

xg2g accepts responsible security disclosures for the active release line and the `main` branch.

## Supported Versions

| Version | Supported |
| :--- | :--- |
| Latest `v3.x` release | Yes |
| `main` branch | Best effort |
| Older major versions | No |

## Reporting a Vulnerability

**DO NOT open a public GitHub issue or public discussion for a suspected security vulnerability.**

xg2g utilizes **GitHub Private Vulnerability Reporting (PVR)** to allow security researchers and operators to report vulnerabilities privately.

To submit a private security advisory:

1. Go to the repository's [Security Advisories](https://github.com/ManuGH/xg2g/security/advisories/new) page.
2. Click **Report a vulnerability**.
3. Provide details including:
   - Affected version or commit SHA
   - Step-by-step reproduction guide or proof-of-concept
   - Potential impact assessment
   - Suggested fix or mitigation (if available)

## Response Expectations

- **Acknowledgement:** Best effort within 48 hours.
- **Triage:** Impact, exposure surface, and severity assessment.
- **Remediation:** Patch release via SemVer tag or documented mitigation.

## Security References

- [Security Operations Guide](docs/ops/SECURITY.md)
- [Scanner Signal Governance](docs/SCANNER_GOVERNANCE.md)
- [Observability & Audit Logging](docs/ops/OBSERVABILITY.md)
