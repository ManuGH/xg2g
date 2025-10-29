# Renovate Setup Guide

This guide walks you through activating and configuring Renovate for automated dependency updates.

## What is Renovate?

Renovate is an intelligent dependency update tool that automatically creates pull requests to keep your dependencies up to date. It's more powerful than Dependabot with features like:

- **Grouped updates**: Related dependencies updated together
- **Digest pinning**: Pins Docker images and GitHub Actions to SHA digests
- **Stability windows**: Waits before updating to avoid breaking changes
- **Automerge**: Automatically merges safe updates
- **Dependency dashboard**: Single issue tracking all updates

## Prerequisites

- Repository admin access
- GitHub account

## Activation Steps

### 1. Install Renovate GitHub App

1. Navigate to https://github.com/apps/renovate
2. Click **Install** or **Configure** (if already installed)
3. Select **Only select repositories** and choose `ManuGH/xg2g`
4. Click **Install** to grant permissions

### 2. Initial Configuration

Renovate will detect the `renovate.json` file in the repository and use it automatically.

The configuration is already optimized for xg2g:

```json
{
  "schedule": ["before 3am on Monday"],
  "prConcurrentLimit": 10,
  "stabilityDays": 3,
  "automerge": true  // For minor/patch updates
}
```

### 3. First Run

Within a few minutes of installation:

1. Renovate runs and analyzes dependencies
2. Creates a **Dependency Dashboard** issue
3. Opens initial PRs for outdated dependencies

**Expected initial PRs:**
- Go module updates (grouped)
- GitHub Actions updates (grouped)
- Docker base image updates

### 4. Verify Installation

Check that Renovate is working:

```bash
# View recent Renovate runs
gh run list --workflow="Renovate" --limit 5

# Check for Dependency Dashboard issue
gh issue list --label "dependencies"

# List Renovate PRs
gh pr list --label "dependencies"
```

## Configuration Overview

### Update Schedules

| Dependency Type | Schedule | Automerge | Stability Days |
|----------------|----------|-----------|----------------|
| Go patch updates | Weekly (Mon 3am UTC) | ✅ Yes | 3 |
| Go minor/major | Weekly (Mon 3am UTC) | ❌ No | 3 |
| GitHub Actions patch/minor | Weekly (Mon 3am UTC) | ✅ Yes | 3 |
| GitHub Actions major | Weekly (Mon 3am UTC) | ❌ No | 3 |
| Docker base images | Weekly (Mon 3am UTC) | ❌ No | 7 |
| **Security updates** | **Immediate** | ❌ No | **0** |

### Automerge Rules

Renovate will automatically merge PRs if:

- ✅ Update is patch or minor (not major)
- ✅ All CI checks pass (CI, tests, security scans)
- ✅ No conflicts with other PRs
- ✅ Stability period passed

**Major updates always require manual review.**

### Grouping Strategy

Related dependencies are grouped together:

- **Go dependencies**: All Go modules in one PR
- **GitHub Actions**: All action updates in one PR
- **Docker base images**: All base image updates in one PR

This reduces PR noise and makes reviewing easier.

## Daily Workflow

### Reviewing Renovate PRs

1. **Check Dependency Dashboard**
   ```bash
   gh issue view $(gh issue list --label "dependencies" --json number -q '.[0].number')
   ```

2. **Review open PRs**
   ```bash
   gh pr list --label "dependencies"
   ```

3. **Merge safe updates**
   ```bash
   # Renovate auto-merges patch/minor updates with passing CI
   # You only need to manually review major updates
   ```

### Handling Major Updates

For major version updates:

1. Review the PR description (Renovate includes changelogs)
2. Check for breaking changes
3. Run tests locally if needed:
   ```bash
   gh pr checkout <pr-number>
   go test ./...
   docker compose up --build
   ```
4. Approve and merge when satisfied

### Emergency Security Updates

Security vulnerabilities are handled immediately:

1. Renovate creates PR with `security` label
2. CI runs automatically
3. **Review and merge ASAP** (no stability delay)

## Monitoring

### Weekly Review Checklist

- [ ] Check Dependency Dashboard for pending updates
- [ ] Review any failed PRs and investigate errors
- [ ] Verify automerged PRs didn't break anything
- [ ] Update `renovate.json` if needed

### Monthly Audit

```bash
# List all Renovate PRs from last month
gh pr list --label "dependencies" --state all --limit 100 \
  --json number,title,mergedAt --jq '.[] | select(.mergedAt != null) | "\(.number): \(.title)"'

# Check Renovate run history
gh run list --workflow="Renovate" --limit 20
```

## Troubleshooting

### Renovate Not Running

**Check installation:**
```bash
gh api /repos/ManuGH/xg2g/installation
```

**Manually trigger run:**
```bash
# Renovate runs automatically, but you can trigger via webhook
# (requires Renovate app webhook access)
```

### PRs Not Auto-Merging

**Common reasons:**
- CI checks failing
- Stability period not passed
- Merge conflicts
- Branch protection rules blocking

**Debug:**
```bash
gh pr view <pr-number> --json statusCheckRollup
```

### Too Many PRs

**Adjust concurrent PR limit** in `renovate.json`:
```json
{
  "prConcurrentLimit": 5  // Reduce from 10
}
```

### Disable Renovate Temporarily

**Option 1: Pause in dashboard issue**
Add comment in Dependency Dashboard:
```
renovate:pause
```

**Option 2: Disable in config**
```json
{
  "enabled": false
}
```

## Migration from Dependabot

If you want to fully migrate from Dependabot to Renovate:

1. **Disable Dependabot**:
   ```bash
   # Edit .github/dependabot.yml
   echo "version: 2" > .github/dependabot.yml
   echo "updates: []" >> .github/dependabot.yml
   git add .github/dependabot.yml
   git commit -m "chore: disable Dependabot (migrated to Renovate)"
   ```

2. **Close existing Dependabot PRs**:
   ```bash
   gh pr list --author "dependabot[bot]" --json number -q '.[].number' | \
     xargs -I {} gh pr close {}
   ```

3. **Let Renovate take over** (it will recreate necessary PRs)

**Note:** You can also run both Dependabot and Renovate together. Renovate will detect and not duplicate Dependabot PRs.

## Advanced Configuration

### Custom Update Schedule

Adjust schedule in `renovate.json`:
```json
{
  "schedule": [
    "after 9pm every weekday",
    "before 6am every weekday",
    "every weekend"
  ]
}
```

### Enable Automerge for More Updates

```json
{
  "packageRules": [
    {
      "matchManagers": ["gomod"],
      "matchUpdateTypes": ["minor"],  // Add minor updates
      "automerge": true
    }
  ]
}
```

### Add Custom Labels

```json
{
  "labels": ["dependencies", "renovate", "automation"]
}
```

## Best Practices

1. **Review Dependency Dashboard weekly**: Keep dependencies fresh
2. **Don't ignore security updates**: Merge immediately
3. **Test major updates locally**: Don't blindly merge breaking changes
4. **Keep Renovate config updated**: Adjust as project evolves
5. **Monitor automerge results**: Ensure CI catches issues

## Resources

- [Renovate Documentation](https://docs.renovatebot.com/)
- [Configuration Options](https://docs.renovatebot.com/configuration-options/)
- [Preset Configs](https://docs.renovatebot.com/presets-default/)
- [GitHub App](https://github.com/apps/renovate)

## Support

- **Renovate Issues**: https://github.com/renovatebot/renovate/issues
- **xg2g Discussions**: https://github.com/ManuGH/xg2g/discussions
- **Configuration Help**: See `renovate.json` comments

---

**Ready to activate?** Follow steps 1-3 above to get started!
