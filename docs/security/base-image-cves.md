# Base Image CVEs

This document tracks known Common Vulnerabilities and Exposures (CVEs) in the base images used by `xg2g` that are currently unpatched or considered acceptable risks.

## Debian Trixie Slim

The `xg2g` runtime image is based on `debian:trixie-slim` with FFmpeg 7.x. While we strive to keep the image updated, some CVEs may persist due to upstream delays or because they do not affect the `xg2g` application context.

### Known Issues

| CVE ID | Severity | Status | Justification |
| :--- | :--- | :--- | :--- |
| **CVE-2023-XXXX** | Low | Ignored | Example: Vulnerability in a package not used by xg2g. |

*(Note: This list is populated based on Trivy scan results. If a CVE is critical or high and has a fix, it should be addressed by upgrading packages in the Dockerfile.)*

### Mitigation Strategy

1. **Regular Updates**: The `Dockerfile` includes `apt-get upgrade -y` to ensure the latest security patches are installed during the build.
2. **Minimal Image**: We use the `slim` variant to reduce the attack surface.
3. **Trivy Scanning**: We run Trivy scans on every release and daily on the main branch to detect new vulnerabilities.
