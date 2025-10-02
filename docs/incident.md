# Incident Response Playbook

This document provides step-by-step procedures for responding to security incidents and production issues.

## Severity Classification

| Severity | Description | Response Time | Examples |
|----------|-------------|---------------|----------|
| **P0 - Critical** | Service down, data breach, active exploit | Immediate | CVE with active exploit, complete service outage |
| **P1 - High** | Major functionality broken, security vulnerability | < 4 hours | High-severity CVE, EPG refresh failure, auth bypass |
| **P2 - Medium** | Degraded performance, minor functionality issue | < 24 hours | Memory leak, slow EPG updates, deprecated dependency |
| **P3 - Low** | Cosmetic issues, documentation errors | < 1 week | Minor UI issues, documentation updates |

## Incident Types

### 1. Security Vulnerability (CVE)

**Scenario**: Dependency has published CVE or govulncheck reports vulnerability

**Inputs**:
- CVE identifier (e.g., CVE-2024-12345)
- Affected package and version
- Severity level (Critical/High/Medium/Low)
- Exploit availability

**Response Steps**:

1. **Assess Impact** (< 30 minutes)
   ```bash
   # Check if vulnerability affects production
   govulncheck ./...

   # Review SBOM for affected dependency
   cat dist/sbom.txt | grep -i <package-name>

   # Check production version
   kubectl get deployment xg2g -o yaml | grep image:
   ```

2. **Immediate Mitigation** (if Critical/High)
   ```bash
   # Option A: Pin to safe version in go.mod
   go get <package>@<safe-version>
   go mod tidy

   # Option B: Rollback to previous release
   kubectl set image deployment/xg2g xg2g=ghcr.io/manugh/xg2g:<previous-tag>
   ```

3. **Hotfix Development**
   ```bash
   # Create hotfix branch from latest tag
   git checkout -b hotfix/cve-2024-12345 v1.2.3

   # Update dependency
   go get <package>@<patched-version>
   go mod tidy

   # Verify fix
   make test
   make security
   govulncheck ./...

   # Commit
   git commit -m "fix(security): patch CVE-2024-12345 in <package>

   Vulnerability: <brief description>
   Severity: <Critical/High/Medium/Low>
   Affected versions: <range>
   Fixed version: <version>

   References:
   - https://cve.mitre.org/cgi-bin/cvename.cgi?name=CVE-2024-12345
   - <vendor advisory URL>
   "
   ```

4. **Testing & Validation**
   ```bash
   # Build and test locally
   make release-build

   # Deploy to staging (if available)
   docker build -t xg2g:hotfix .
   docker run -d --name xg2g-test xg2g:hotfix

   # Run smoke tests
   curl http://localhost:8080/healthz
   curl http://localhost:8080/api/status
   ```

5. **Release & Deploy**
   ```bash
   # Push hotfix branch
   git push origin hotfix/cve-2024-12345

   # Create PR (will require CI + review even for hotfix)
   gh pr create -B main -t "fix(security): patch CVE-2024-12345" \
     -b "**Severity**: Critical/High/Medium/Low
   **CVE**: CVE-2024-12345
   **Package**: <package-name>
   **Fixed Version**: <version>

   ## Impact
   <describe impact>

   ## Mitigation
   Updated dependency to patched version.

   ## References
   - [CVE-2024-12345](https://cve.mitre.org/cgi-bin/cvename.cgi?name=CVE-2024-12345)"

   # After merge: Create patch release tag
   git checkout main
   git pull
   git tag -a v1.2.4 -m "Release v1.2.4 - Security patch for CVE-2024-12345"
   git push origin v1.2.4

   # Verify release assets created
   gh release view v1.2.4

   # Deploy to production
   kubectl set image deployment/xg2g xg2g=ghcr.io/manugh/xg2g:v1.2.4
   kubectl rollout status deployment/xg2g
   ```

6. **Post-Incident**
   - Update [SECURITY.md](../SECURITY.md) if needed
   - Document in [CHANGELOG.md](../CHANGELOG.md) with `[SECURITY]` prefix
   - Review Dependabot config to prevent similar issues

---

### 2. Faulty Dependency Update

**Scenario**: Dependabot PR or manual dependency update breaks tests/build

**Inputs**:
- Dependency name and version
- Failure type (build error, test failure, regression)
- PR number (if Dependabot)

**Response Steps**:

1. **Isolate**
   ```bash
   # Close Dependabot PR without merge
   gh pr close <PR-number>

   # Or revert merged dependency update
   git revert <commit-sha>
   ```

2. **Pin to Last Known Good Version**
   ```bash
   # In go.mod, explicitly pin dependency
   go get <package>@<last-good-version>
   go mod tidy

   # Verify
   make test
   make lint
   ```

3. **Investigation**
   ```bash
   # Check dependency changelog
   gh repo view <org>/<repo> --web

   # Review breaking changes
   git diff go.mod go.sum

   # Run specific tests
   go test -v -run <TestName> ./...
   ```

4. **Resolution**
   - **Short-term**: Keep pinned version, document in go.mod comment
   - **Long-term**: Create issue to track upgrade path, update code if needed

---

### 3. EPG Refresh Failure

**Scenario**: EPG data not updating, stale programme information

**Inputs**:
- Error messages from logs
- Last successful refresh timestamp
- OpenWebIF endpoint status

**Response Steps**:

1. **Diagnose**
   ```bash
   # Check application logs
   docker logs xg2g | grep -i epg
   kubectl logs deployment/xg2g | grep -i epg

   # Check EPG refresh job status
   curl http://localhost:8080/api/status | jq '.last_refresh'

   # Verify OpenWebIF connectivity
   curl -v $XG2G_OWI_BASE/api/getallservices
   ```

2. **Common Causes**
   - OpenWebIF endpoint unreachable → Network/DNS issue
   - Authentication failure → Check credentials
   - Rate limiting → Adjust `XG2G_REFRESH_INTERVAL`
   - Parsing error → Check OpenWebIF API response format

3. **Immediate Fix**
   ```bash
   # Manual trigger (if API supports it)
   curl -X POST http://localhost:8080/api/refresh

   # Or restart application (triggers immediate refresh)
   kubectl rollout restart deployment/xg2g
   ```

4. **Long-term Fix**
   ```bash
   # Add retry logic (if not present)
   # Add better error logging
   # Add Prometheus alerts for failed refreshes
   ```

---

### 4. Production Outage

**Scenario**: Service completely unavailable

**Response Steps**:

1. **Immediate Triage** (< 5 minutes)
   ```bash
   # Check service status
   curl http://localhost:8080/healthz

   # Check container status
   docker ps | grep xg2g
   kubectl get pods -l app=xg2g

   # Check recent logs
   docker logs --tail 100 xg2g
   kubectl logs -l app=xg2g --tail=100
   ```

2. **Quick Recovery** (< 10 minutes)
   ```bash
   # Option A: Restart
   docker restart xg2g
   kubectl rollout restart deployment/xg2g

   # Option B: Rollback to previous version
   kubectl rollout undo deployment/xg2g

   # Option C: Scale up (if single instance)
   kubectl scale deployment/xg2g --replicas=2
   ```

3. **Root Cause Analysis**
   - Check resource limits (CPU, memory, disk)
   - Review recent deployments
   - Check external dependencies (OpenWebIF)
   - Review application logs for panics/errors

---

## Communication Templates

### Security Advisory (GitHub Security Advisory)

```markdown
## Summary
[Brief description of vulnerability]

## Severity
Critical / High / Medium / Low

## Affected Versions
- Versions < v1.2.4

## Patched Versions
- v1.2.4 and later

## Impact
[Describe what an attacker could do]

## Workaround
[If available, describe temporary mitigation]

## Fix
Upgrade to v1.2.4 or later:
- Docker: `docker pull ghcr.io/manugh/xg2g:v1.2.4`
- Binary: Download from [GitHub Releases](https://github.com/ManuGH/xg2g/releases/tag/v1.2.4)

## References
- CVE-YYYY-XXXXX
- [Vendor Advisory URL]
```

### Changelog Entry (CHANGELOG.md)

```markdown
## [1.2.4] - 2024-XX-XX

### Security
- **[SECURITY]** Fix CVE-2024-12345 in dependency `package/name` (Critical severity)
  - Updated `package/name` from v1.0.0 to v1.0.1
  - Impact: [describe impact]
  - All users should upgrade immediately

### Fixed
- EPG refresh now retries on transient network errors
```

### Incident Postmortem (docs/postmortems/YYYY-MM-DD-incident-name.md)

```markdown
# Incident Postmortem: [Incident Name]

**Date**: YYYY-MM-DD
**Duration**: X hours
**Severity**: P0/P1/P2/P3
**Status**: Resolved

## Summary
[One paragraph summary of what happened]

## Timeline (UTC)
- **HH:MM** - Incident detected
- **HH:MM** - Initial investigation started
- **HH:MM** - Root cause identified
- **HH:MM** - Fix deployed
- **HH:MM** - Service restored
- **HH:MM** - Incident closed

## Root Cause
[Technical explanation of what went wrong]

## Impact
- Users affected: [number/percentage]
- Services affected: [list]
- Data loss: Yes/No

## Resolution
[What was done to fix it]

## Lessons Learned

### What Went Well
- [Positive aspects of response]

### What Went Wrong
- [Issues during response]

### Action Items
- [ ] [Action item 1] - Owner: @user - Due: YYYY-MM-DD
- [ ] [Action item 2] - Owner: @user - Due: YYYY-MM-DD
```

---

## Escalation

1. **Internal**: Check [SUPPORT.md](../SUPPORT.md) for maintainer contact
2. **External**: For critical infrastructure (GitHub, Docker Hub, etc.), check their status pages
3. **Community**: Post in GitHub Discussions for non-sensitive issues

## Prevention

- **Automated Monitoring**: Set up Prometheus alerts for health check failures
- **Regular Drills**: Test rollback procedures quarterly
- **Dependency Review**: Review Dependabot PRs before merging
- **Security Scanning**: Keep govulncheck and CodeQL enabled in CI
