# Security Policy

## Supported Versions

We release patches for security vulnerabilities in the following versions:

| Version | Supported          |
| ------- | ------------------ |
| latest  | :white_check_mark: |
| < latest| :x:                |

We support only the latest release. Please upgrade to the most recent version to receive security updates.

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

Instead, please report them via one of the following channels:

1. **GitHub Security Advisories** (preferred):
   - Go to https://github.com/ManuGH/xg2g/security/advisories
   - Click "Report a vulnerability"
   - Provide detailed information about the vulnerability

2. **Email**: Contact the maintainer directly at the email address listed in the repository

### What to Include

Please include the following information in your report:

- Type of vulnerability (e.g., injection, authentication bypass, etc.)
- Full paths of source file(s) related to the vulnerability
- Location of the affected source code (tag/branch/commit or direct URL)
- Step-by-step instructions to reproduce the issue
- Proof-of-concept or exploit code (if possible)
- Impact of the vulnerability, including how an attacker might exploit it

### Response Timeline

- **Initial Response**: Within 48 hours of report submission
- **Triage & Assessment**: Within 5 business days
- **Fix Development**: Depends on severity
  - Critical: Within 7 days
  - High: Within 14 days
  - Medium: Within 30 days
  - Low: Next scheduled release
- **Public Disclosure**: After fix is released and users have had time to update (typically 7-14 days)

## Security Measures

This project implements the following security practices:

- **Automated Security Scanning**:
  - `govulncheck` - Go vulnerability scanner (fails on High/Critical)
  - `gosec` - Go security analyzer
  - CodeQL - GitHub Advanced Security
  - Trivy - Container vulnerability scanning

- **Dependency Management**:
  - Automated weekly Dependabot updates
  - `go mod verify` on every build
  - SBOM generation (SPDX + CycloneDX)

- **CI/CD Security**:
  - All GitHub Actions pinned to commit SHA
  - Required status checks enforced
  - Branch protection with admin enforcement
  - CODEOWNERS review requirement

- **Runtime Security**:
  - Non-root container execution
  - Read-only root filesystem
  - No privilege escalation
  - Health check endpoints

## Security-Related Configuration

### Environment Variables

Sensitive configuration should be provided via environment variables or Kubernetes secrets:

- `XG2G_OWI_BASE` - OpenWebIF endpoint (may contain credentials)
- `XG2G_BASICAUTH_USER` / `XG2G_BASICAUTH_PASS` - HTTP Basic Auth credentials

**Never commit credentials to the repository.**

### Authentication

When HTTP Basic Auth is enabled:
- Use strong passwords (minimum 16 characters recommended)
- Rotate credentials regularly
- Use HTTPS in production to prevent credential interception

## Incident Response

In case of a security incident, see [docs/incident.md](docs/incident.md) for the detailed incident response playbook.

## Security Updates

Security updates are announced via:
- GitHub Security Advisories
- Release notes with `[SECURITY]` prefix
- CHANGELOG.md with detailed impact description
