# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 2.0.x   | ✅ Supported       |
| 1.x     | ❌ End of Life     |

## Reporting a Vulnerability

We take the security of `xg2g` seriously. If you believe you have found a security vulnerability in `xg2g`, please report it to us as described below.

**Please do not report security vulnerabilities through public GitHub issues.**

Instead, please report them via **GitHub Security Advisories** (Draft a new advisory). This allows us to discuss and fix the issue privately before disclosure.

## Vulnerability Response Process

1. **Triage**: We will acknowledge receipt of your report within 48 hours and begin investigating the issue.
2. **Confirmation**: We will confirm whether the issue is a vulnerability and assess its severity.
3. **Fix**: We will work on a fix for the vulnerability.
4. **Release**: We will release a patched version of `xg2g`.
5. **Disclosure**: We will publish a security advisory and credit you for the discovery (if you wish).

## Known Issues & Accepted Risks

### Base Image CVEs

The project uses Debian 13 (Trixie) base image. Some CVEs may exist in base image packages
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

## Security Guarantees

As of version 2.0+, `xg2g` enforces the following security invariants by default:

1. **Strict File Serving**: Arbitrary file access via the API is forbidden. Only specific, required playback files (M3U, XMLTV) are served.
2. **Localhost-Only Internal Services**: The stream proxy and metrics endpoints bind to `127.0.0.1` by default preventing accidental exposure.
3. **Authenticated Proxying**: Access to the stream proxy requires a valid API token.
4. **SSRF Protection**: Upstream proxy targets are strictly validated against configured allowlists.
