# Migration and Upgrade Guide

This guide covers upgrading xg2g between versions and handling breaking changes.

## Version Compatibility

xg2g follows semantic versioning (MAJOR.MINOR.PATCH):

- **PATCH** (1.0.x → 1.0.y): Bug fixes, backward compatible
- **MINOR** (1.x → 1.y): New features, backward compatible
- **MAJOR** (x.0.0 → y.0.0): Breaking changes, may require migration

## Current Version: 1.x.x

No migrations required. All changes are backward compatible.

## Upgrade Process

### Docker

```bash
# Pull new image
docker pull ghcr.io/manuGH/xg2g:latest

# Stop and remove old container
docker stop xg2g
docker rm xg2g

# Start with same configuration
docker run -d --name xg2g \
  -v xg2g-data:/data \
  -e XG2G_OWI_BASE=http://receiver.local \
  # ... other env vars ...
  ghcr.io/manuGH/xg2g:latest
```

### Kubernetes

```bash
# Update image in deployment
kubectl set image deployment/xg2g xg2g=ghcr.io/manugh/xg2g:v1.3.0

# Or update via manifest
kubectl apply -f deployment.yaml
```

## Breaking Changes Log

### v2.0.0 (Future)

**Planned changes:**
- Configuration file format change (YAML v2)
- API endpoint restructuring
- Database schema for persistent state

**Migration path will include:**
1. Automatic config conversion tool
2. API compatibility layer for v1 endpoints
3. Data migration scripts

## Pre-Upgrade Checklist

Before upgrading to a new major version:

1. ✅ **Backup current data**
   ```bash
   docker cp xg2g:/data ./backup-$(date +%Y%m%d)
   ```

2. ✅ **Review release notes**
   - Check breaking changes
   - Review new features
   - Note deprecated functionality

3. ✅ **Test in staging**
   - Deploy to test environment first
   - Verify all integrations work
   - Test API consumers

4. ✅ **Plan rollback**
   - Keep old image tag handy
   - Document rollback procedure
   - Test rollback in staging

## Post-Upgrade Steps

After successful upgrade:

1. **Verify Health**
   ```bash
   curl http://localhost:8080/healthz
   curl http://localhost:8080/readyz
   ```

2. **Trigger Refresh**
   ```bash
   curl -X POST http://localhost:8080/api/refresh
   ```

3. **Check Logs**
   ```bash
   docker logs xg2g --tail 100
   ```

4. **Monitor Metrics**
   - Check Prometheus metrics
   - Verify Grafana dashboards
   - Review error rates

## Rollback Procedure

If issues occur after upgrade:

```bash
# Stop new version
docker stop xg2g
docker rm xg2g

# Restore previous version
docker run -d --name xg2g \
  -v xg2g-data:/data \
  ghcr.io/manugh/xg2g:v1.2.0  # Previous version

# Verify rollback
curl http://localhost:8080/healthz
```

## Configuration Migration

### Environment Variables

Current environment variables are stable. Future changes will:
- Maintain backward compatibility for 1 major version
- Provide deprecation warnings in logs
- Include automatic migration hints

### Config File Format

If using `config.yaml`:
- Format is stable in v1.x
- Future v2.0 will include conversion tool
- Both formats will be supported during transition

## Database Schema (Future)

v2.0 will introduce optional persistent storage:
- SQLite for metadata and state
- Migration tool: `xg2g migrate`
- Automatic backup before migration
- Rollback support

Example migration flow (future):
```bash
# Check current schema version
xg2g version --schema

# Dry-run migration
xg2g migrate --dry-run

# Perform migration
xg2g migrate

# Verify migration
xg2g migrate --verify
```

## API Versioning

xg2g uses URL-based API versioning:
- `/api/v1/*` - Version 1 (current)
- `/api/v2/*` - Version 2 (future)
- Legacy endpoints redirect to v1

API compatibility promise:
- v1 API supported for 12 months after v2 release
- Deprecation warnings in response headers
- Migration guide published with v2

## Zero-Downtime Updates

For production deployments:

1. **Rolling Update** (Kubernetes)
   ```yaml
   strategy:
     type: RollingUpdate
     rollingUpdate:
       maxUnavailable: 0
       maxSurge: 1
   ```

2. **Blue-Green Deployment**
   - Deploy new version alongside old
   - Switch traffic after validation
   - Keep old version for quick rollback

3. **Canary Deployment**
   - Route 10% traffic to new version
   - Monitor error rates
   - Gradually increase to 100%

## Support Policy

- **Latest version**: Full support, bug fixes, security patches
- **Previous minor**: Security patches only (6 months)
- **Older versions**: Community support, no official patches

## Getting Help

- Documentation: https://github.com/ManuGH/xg2g/docs
- Issues: https://github.com/ManuGH/xg2g/issues
- Discussions: https://github.com/ManuGH/xg2g/discussions
