# Supply Chain Security Tools

This document describes the supply chain security tools integrated into xg2g.

## Overview

xg2g implements a comprehensive supply chain security strategy using:

1. **OpenSSF Scorecard** - Automated security scoring
2. **Renovate** - Intelligent dependency updates
3. **Cosign** - Container image signing
4. **SLSA** - Build provenance attestation
5. **Conftest** - Policy enforcement
6. **Fuzzing** - Automated vulnerability discovery

## OpenSSF Scorecard

### What is it?

[OpenSSF Scorecard](https://github.com/ossf/scorecard) is an automated tool that assesses open source projects for security risks through a series of checks.

### Configuration

See [.github/workflows/scorecard.yml](../../.github/workflows/scorecard.yml)

### Checks Performed

| Check | Description | Current Status |
|-------|-------------|----------------|
| **Binary-Artifacts** | Detects binary artifacts in the repository | ✅ Expected to pass |
| **Branch-Protection** | Checks if branch protection is enabled | ⚠️ Needs manual setup |
| **CI-Tests** | Verifies CI tests run on all commits | ✅ Passing |
| **CII-Best-Practices** | Checks for CII Best Practices badge | ⏳ Optional |
| **Code-Review** | Verifies code review before merging | ✅ Expected with branch protection |
| **Contributors** | Checks for multiple contributors | ✅ Passing |
| **Dangerous-Workflow** | Detects dangerous GitHub Actions patterns | ✅ Passing |
| **Dependency-Update-Tool** | Checks for automated dependency updates | ✅ Renovate + Dependabot |
| **Fuzzing** | Checks for fuzzing integration | ✅ Go native fuzzing enabled |
| **License** | Verifies project has a license | ✅ MIT License |
| **Maintained** | Checks if project is actively maintained | ✅ Passing |
| **Packaging** | Checks for published packages | ✅ GitHub Container Registry |
| **Pinned-Dependencies** | Verifies dependencies are pinned | ✅ Renovate handles this |
| **SAST** | Checks for static analysis tools | ✅ CodeQL, gosec, golangci-lint |
| **Security-Policy** | Verifies SECURITY.md exists | ✅ Present |
| **Signed-Releases** | Checks for signed releases | ✅ Cosign signatures |
| **Token-Permissions** | Checks GitHub Actions token permissions | ✅ Minimal permissions set |
| **Vulnerabilities** | Checks for known vulnerabilities | ✅ govulncheck, Trivy |

### Viewing Results

Results are published to:
- **GitHub Security tab**: Code scanning alerts
- **OpenSSF API**: Public scorecard badge
- **Artifacts**: SARIF file in workflow runs

### Badge

Add to README.md:

```markdown
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/ManuGH/xg2g/badge)](https://securityscorecards.dev/viewer/?uri=github.com/ManuGH/xg2g)
```

## Renovate

### What is it?

[Renovate](https://docs.renovatebot.com/) is an automated dependency update tool with advanced features beyond Dependabot.

### Configuration

See [renovate.json](../../renovate.json)

### Key Features

**Advantages over Dependabot:**
- 🎯 **Grouped updates**: Related dependencies updated together
- 📦 **Monorepo support**: Handles multi-module projects
- 🔐 **Digest pinning**: Pins Docker images and GitHub Actions to SHA
- ⏱️ **Stability days**: Waits before updating to avoid breaking changes
- 🤖 **Automerge**: Automatically merges safe updates
- 📊 **Dependency dashboard**: Single issue tracking all updates

### Update Strategy

| Dependency Type | Update Frequency | Automerge | Stability Days |
|----------------|------------------|-----------|----------------|
| **Go patch** | Weekly | ✅ Yes | 3 |
| **Go minor/major** | Weekly | ❌ No | 3 |
| **GitHub Actions patch/minor** | Weekly | ✅ Yes | 3 |
| **GitHub Actions major** | Weekly | ❌ No | 3 |
| **Docker base images** | Weekly | ❌ No | 7 |
| **Security updates** | Immediate | ❌ No | 0 |

### Activation

1. Install [Renovate GitHub App](https://github.com/apps/renovate)
2. Grant repository access
3. Renovate will automatically detect `renovate.json`
4. First run creates a "Dependency Dashboard" issue

### Migration from Dependabot

Renovate and Dependabot can coexist, but it's recommended to disable Dependabot to avoid duplicate PRs:

```yaml
# .github/dependabot.yml
version: 2
updates: []  # Disable all Dependabot updates
```

## Cosign + SLSA

### Image Verification

All container images are signed with Cosign and include SLSA provenance:

```bash
# Verify signature
cosign verify \
  --certificate-identity-regexp="https://github.com/ManuGH/xg2g" \
  ghcr.io/manugh/xg2g:latest

# View build provenance
cosign verify-attestation --type slsaprovenance \
  ghcr.io/manugh/xg2g:latest | jq '.payload | @base64d | fromjson'
```

### What This Proves

- ✅ **Authenticity**: Image was built by official GitHub Actions
- ✅ **Integrity**: No tampering since build
- ✅ **Provenance**: Complete build metadata (commit, workflow, builder)
- ✅ **Non-repudiation**: Timestamped signature from Sigstore

## Conftest (OPA Policies)

### What is it?

[Conftest](https://www.conftest.dev/) uses Open Policy Agent (OPA) to enforce security policies on configuration files.

### Policies Enforced

See [policy/](../../policy/)

**Dockerfile policies:**
- Non-root USER required
- No `latest` tags
- HEALTHCHECK recommended
- Clean apt-get installations

**Kubernetes policies:**
- runAsNonRoot required
- Resource limits/requests required
- No privileged containers
- allowPrivilegeEscalation: false

### Local Testing

```bash
# Install conftest
brew install conftest

# Test Dockerfile
pip install dockerfile-parse
python3 << 'EOF'
import json
from dockerfile_parse import DockerfileParser

with open('Dockerfile', 'r') as f:
    dfp = DockerfileParser()
    dfp.content = f.read()

output = [{"Cmd": insn['instruction'].lower(), "Value": insn['value'].split()}
          for insn in dfp.structure]

with open('dockerfile.json', 'w') as f:
    json.dump(output, f)
EOF

conftest test dockerfile.json --policy policy/dockerfile.rego
```

## Fuzzing

### What is it?

Go's native fuzzing generates random inputs to discover edge cases and crashes.

### Running Locally

```bash
# Fuzz XMLTV parsing for 1 minute
cd internal/epg
go test -fuzz=FuzzBuildNameToIDMap -fuzztime=1m

# View crash corpus
ls testdata/fuzz/FuzzBuildNameToIDMap/
```

### CI Integration

See [.github/workflows/fuzz.yml](../../.github/workflows/fuzz.yml)

- Runs weekly on Monday at 2am UTC
- 2 minutes per fuzz function
- Crash artifacts uploaded to GitHub

## Monitoring

### Weekly Review

```bash
# Check Scorecard results
gh run list --workflow=scorecard.yml --limit 1
gh run view $(gh run list --workflow=scorecard.yml --limit 1 --json databaseId -q '.[0].databaseId')

# Review Renovate PRs
gh pr list --label dependencies

# Check fuzzing crashes
gh run list --workflow=fuzz.yml --limit 5
```

### Alerts

Enable GitHub notifications for:
- ✅ Dependabot/Renovate security updates
- ✅ CodeQL findings
- ✅ Fuzzing crashes
- ✅ Workflow failures

## Best Practices

1. **Review Renovate Dashboard weekly**: Check for pending updates
2. **Investigate Scorecard drops**: Score should stay > 7.0
3. **Triage fuzzing crashes immediately**: May indicate real vulnerabilities
4. **Keep policies updated**: Add new conftest rules as threats evolve
5. **Rotate tokens regularly**: See [token-rotation-2025-10.md](token-rotation-2025-10.md)

## References

- [OpenSSF Best Practices](https://www.bestpractices.dev/)
- [SLSA Framework](https://slsa.dev/)
- [Sigstore Documentation](https://docs.sigstore.dev/)
- [Renovate Documentation](https://docs.renovatebot.com/)
- [Go Fuzzing](https://go.dev/security/fuzz/)
