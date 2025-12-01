# Security Policy

## Overview

xg2g implements a comprehensive DevSecOps pipeline with automated security scanning at multiple layers.
All security tools run automatically on every commit, pull request, and daily via scheduled scans.

## Supported Versions

We release patches for security vulnerabilities in the following versions:

| Version | Supported          |
| ------- | ------------------ |
| latest  | :white_check_mark: |
| < latest| :x:                |

We support only the latest release. Please upgrade to the most recent version to receive security updates.

## 🛡️ Security Layers

### 1. Static Code Analysis

| Tool | Scope | Frequency | Blocking |
|------|-------|-----------|----------|
| [CodeQL](https://github.com/github/codeql) | Go, C/C++ | PR, Push, Weekly | Yes |
| [gosec](https://github.com/securego/gosec) | Go Security | PR, Push | Yes (Medium+) |
| [govulncheck](https://golang.org/x/vuln/cmd/govulncheck) | Go Vulnerabilities | PR, Push | Yes |

**Coverage**: Source code, dependencies, known vulnerabilities

### 2. Container Security

| Tool | Scope | Frequency | Blocking |
|------|-------|-----------|----------|
| [Trivy](https://github.com/aquasecurity/trivy) | Container Images | PR, Push, Daily | Yes (Critical/High) |
| Trivy Config | Dockerfile Best Practices | PR, Push, Daily | No |
| Trivy SBOM | Software Bill of Materials | PR, Push, Daily | No |

**Coverage**: OS packages, application dependencies, Docker configuration, license compliance

**Categories**:

- `container-production`: Multi-stage build (Go + Rust)
- `filesystem-secrets`: Filesystem secret scanning

### 3. Secret Detection

| Tool | Scope | Frequency | Blocking |
|------|-------|-----------|----------|
| [Gitleaks](https://github.com/gitleaks/gitleaks) | Repository History | PR, Push, Daily | Yes |
| Trivy Filesystem | Secrets in Files | PR, Push, Daily | Yes |

**Coverage**: Git history, configuration files, hardcoded credentials

### 4. Supply Chain Security

| Tool | Scope | Frequency | Blocking |
|------|-------|-----------|----------|
| [Scorecard](https://github.com/ossf/scorecard) | OSSF Best Practices | PR, Push | No |
| [Dependabot](https://docs.github.com/en/code-security/dependabot) | Dependency Updates | Weekly | No |
| SBOM Tracking | Dependency Changes | Every Build | No |

**Coverage**: Pinned dependencies, token permissions, branch protection, vulnerability disclosure

### 5. Dependency Management

- **GitHub Actions**: All pinned to commit SHAs with version comments
- **Docker Images**: All pinned to SHA256 digests
- **Go Modules**: Managed via Dependabot
- **Automated Updates**: Weekly Dependabot PRs for all ecosystems

## 🚨 Security Policy & Blocking Rules

### Blocking Policies

The following conditions **block** merges and releases:

1. **Critical/High Vulnerabilities** (Trivy)
   - Any CRITICAL severity vulnerability in production images
   - HIGH severity vulnerabilities that are fixable
   - `--exit-code 1` on Critical findings

2. **Code Security Issues** (gosec)
   - Medium or higher severity findings
   - Excludes: Tests (`*_test.go`), generated code

3. **Known Go Vulnerabilities** (govulncheck)
   - Any vulnerability in `go.mod` dependencies
   - Fails build on detected vulnerabilities

4. **Exposed Secrets** (Gitleaks)
   - Any secret detected in commit history or files
   - Fails immediately on detection

### Non-Blocking (Monitoring Only)

- Docker configuration issues (Trivy Config)
- Scorecard recommendations
- SBOM changes
- Unfixed vulnerabilities without patches available (`ignore-unfixed: true`)

## 📊 Scan Schedule

| Trigger | Workflows |
|---------|-----------|
| **Pull Request** | CodeQL, gosec, govulncheck, Container Security |
| **Push to main** | All security workflows |
| **Daily (2 AM UTC)** | Container Security (full scan) |
| **Weekly (Sunday)** | CodeQL Deep Scan |

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

### For Security Vulnerabilities

Instead, please report them via one of the following channels:

1. **GitHub Security Advisories** (Preferred)
   - Navigate to [Security → Advisories](../../security/advisories)
   - Click "Report a vulnerability"
   - Provide detailed information

2. **Private Disclosure**
   - Contact: [@ManuGH](https://github.com/ManuGH)
   - Include: Description, reproduction steps, impact assessment

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
- **Fix Development**:
  - **Critical** (RCE, Auth Bypass): Within 7 days
  - **High** (Data Exposure): Within 14 days
  - **Medium**: Within 30 days
  - **Low**: Next scheduled release
- **Public Disclosure**: After fix is released and users have had time to update (typically 7-14 days)

### Disclosure Policy

- We follow **coordinated disclosure**
- 90-day embargo for critical issues
- Public disclosure after fix is released
- Credit given to reporters (unless anonymity requested)

## 🏗️ Security Architecture

### Container Hardening

- Multi-stage builds minimize attack surface
- Distroless runtime images (no shell, no package manager)
- Non-root user execution where possible
- Minimal base images (Alpine, Distroless)
- Read-only root filesystems

### CI/CD Security

- **Principle of Least Privilege**: Token permissions explicitly scoped per job
- **Immutable Dependencies**: All actions and images pinned to SHAs/digests
- **Secret Management**: No secrets in code, GitHub Secrets for credentials
- **Branch Protection**: Required status checks, no force pushes

### Build Reproducibility

- `SOURCE_DATE_EPOCH=0` for deterministic builds
- SBOM generation for every release (CycloneDX format)
- Build provenance tracking
- SBOM diff tracking between builds

## 🔐 Security-Related Configuration

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

## 🔍 Security Monitoring

### GitHub Security Features

- **Code Scanning**: CodeQL, gosec, Trivy results
- **Dependabot Alerts**: Automatic vulnerability detection
- **Secret Scanning**: GitHub native + Gitleaks

### Access Security Dashboard

1. Navigate to [Security Tab](../../security)
2. View:
   - **Code Scanning Alerts**: Current findings
   - **Dependabot**: Dependency vulnerabilities
   - **Security Advisories**: Published CVEs

## Incident Response

In case of a security incident:

1. Report immediately via GitHub Security Advisories
2. Follow coordinated disclosure timeline
3. Monitor [Release Notes](../../releases) for security updates

## Security Updates

Security updates are announced via:

- GitHub Security Advisories
- Release notes with `[SECURITY]` prefix
- CHANGELOG.md with detailed impact description

## 📚 Additional Resources

- [OWASP Top 10](https://owasp.org/www-project-top-ten/)
- [CIS Docker Benchmark](https://www.cisecurity.org/benchmark/docker)
- [OSSF Scorecard](https://github.com/ossf/scorecard)
- [SLSA Framework](https://slsa.dev/)

## 🙏 Acknowledgments

This security setup follows industry best practices from:

- OSSF (Open Source Security Foundation)
- CNCF (Cloud Native Computing Foundation)
- OWASP (Open Web Application Security Project)

---

**Last Updated**: 2025-12-01
**Security Contact**: [@ManuGH](https://github.com/ManuGH)
