# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 3.0.x   | :white_check_mark: |
| < 3.0   | :x:                |

## Reporting a Vulnerability

We take the security of `xg2g` seriously. If you believe you have found a security vulnerability in `xg2g`, please report it to us as described below.

**Please do not report security vulnerabilities through public GitHub issues.**

Instead, please report them via email to [security@example.com](mailto:security@example.com). You should receive a response within 48 hours. If for some reason you do not, please follow up via email to ensure we received your original message.

## Vulnerability Response Process

1. **Triage**: We will acknowledge receipt of your report within 48 hours and begin investigating the issue.
2. **Confirmation**: We will confirm whether the issue is a vulnerability and assess its severity.
3. **Fix**: We will work on a fix for the vulnerability.
4. **Release**: We will release a patched version of `xg2g`.
5. **Disclosure**: We will publish a security advisory and credit you for the discovery (if you wish).

## Known Issues & Accepted Risks

### Base Image CVEs

The project uses Debian 12 (Bookworm) and Alpine Linux base images. Some CVEs exist in base image packages
without upstream fixes:

- **Monitoring**: We run daily Trivy scans to detect new vulnerabilities
- **Policy**: Base images are updated regularly when fixes become available
- **Risk Assessment**: These CVEs have no direct attack surface in xg2g's usage
- **Mitigation**:
  - Distroless images where possible (minimal attack surface)
  - Network isolation in production deployments
  - Regular base image updates via Dependabot

### OSSF Scorecard Findings

Some OSSF Scorecard recommendations are accepted risks due to maintenance overhead:

- **Dockerfile Package Pinning**: apk/apt packages not individually pinned
- **Rationale**: Base images are pinned to SHA256 digests, providing reproducibility
- **Mitigation**: Daily vulnerability scanning catches issues in unpinned packages

These risks are documented, monitored, and periodically reassessed.

## Security Best Practices

We recommend the following best practices for running `xg2g`:

- **Run as non-root**: The Docker image is designed to run as a non-root user. Do not override this unless absolutely necessary.
- **Network Isolation**: Run `xg2g` in an isolated network segment if possible.
- **Keep Updated**: Always run the latest supported version of `xg2g`.
