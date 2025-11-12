# Docker Compose Usage Guide

This guide explains how to use the xg2g Docker Compose files efficiently using overlay patterns.

## Quick Reference

```bash
# Standard mode (baseline)
docker compose up -d

# With GPU transcoding
docker compose -f docker-compose.yml -f docker-compose.gpu.yml up -d

# With monitoring stack
docker compose -f docker-compose.yml -f docker-compose.monitoring.yml up -d

# Full stack (GPU + monitoring + tracing)
docker compose \
  -f docker-compose.yml \
  -f docker-compose.gpu.yml \
  -f docker-compose.monitoring.yml \
  -f docker-compose.jaeger.yml \
  up -d
```

## File Organization

### Base Configuration
- **docker-compose.yml** - Core xg2g service (standard mode)
  - No transcoding
  - Original audio streams (AC3/MP2)
  - HDHomeRun emulation
  - Smart stream detection

### Feature Overlays
- **docker-compose.audio-proxy.yml** - Audio transcoding for iOS devices
  - AC3 → AAC conversion
  - Enables iPhone/iPad compatibility

- **docker-compose.gpu.yml** - GPU-accelerated transcoding
  - NVIDIA CUDA support
  - Video + audio transcoding
  - Requires: NVIDIA Container Toolkit

- **docker-compose.jaeger.yml** - Distributed tracing
  - Jaeger all-in-one (UI + collector)
  - OpenTelemetry integration
  - Access: http://localhost:16686

### Environment-Specific
- **docker-compose.monitoring.yml** - Observability stack
  - Prometheus (metrics)
  - Grafana (dashboards)
  - Pre-configured scrapers

- **docker-compose.staging.yml** - Staging environment
  - Test receivers
  - Debug logging
  - Experimental features

- **docker-compose.production.yml** - Production hardening
  - Resource limits
  - Health checks
  - Security policies

### Testing
- **docker-compose.minimal.yml** - Minimal setup for CI
- **docker-compose.test.yml** - Integration test environment

## Common Combinations

### Development Setup
```bash
docker compose \
  -f docker-compose.yml \
  -f docker-compose.monitoring.yml \
  -f docker-compose.jaeger.yml \
  up -d
```

**Includes**: Core service + metrics + tracing

---

### iOS/Apple TV Deployment
```bash
docker compose \
  -f docker-compose.yml \
  -f docker-compose.audio-proxy.yml \
  up -d
```

**Purpose**: Audio transcoding for devices requiring AAC

---

### High-Performance Setup (NVIDIA GPU)
```bash
docker compose \
  -f docker-compose.yml \
  -f docker-compose.gpu.yml \
  -f docker-compose.monitoring.yml \
  up -d
```

**Purpose**: GPU transcoding with monitoring

---

### Production Deployment
```bash
docker compose \
  -f docker-compose.yml \
  -f docker-compose.production.yml \
  -f docker-compose.monitoring.yml \
  up -d
```

**Purpose**: Hardened production with observability

---

## Environment Variables

All compose files support `.env` configuration:

```bash
# .env example
XG2G_OWI_BASE=http://192.168.1.100
XG2G_BOUQUET=Premium
XG2G_LISTEN=:8080
XG2G_LOG_LEVEL=info
XG2G_METRICS_LISTEN=:9090

# GPU-specific
NVIDIA_VISIBLE_DEVICES=0

# Monitoring
GF_SECURITY_ADMIN_PASSWORD=secure_password
```

## Overlay Pattern Best Practices

### ✅ DO:
1. **Use base + selective overlays**: Start with `docker-compose.yml`, add features as needed
2. **Layer by concern**: Features, environment, monitoring
3. **Override sparingly**: Most settings should extend, not replace
4. **Version lock**: Use `image: ghcr.io/manugh/xg2g:v1.7.0` in production

### ❌ DON'T:
1. **Don't edit base file**: Use overlays for customizations
2. **Don't duplicate services**: Use `extends` if needed (not currently used)
3. **Don't mix environments**: Don't combine staging + production overlays

## Health Checks

All compose files include health checks:

```bash
# Check service health
docker compose ps

# View logs
docker compose logs -f xg2g

# Check health endpoint
curl http://localhost:8080/healthz
```

## Resource Management

Resource limits are defined per overlay:

| Overlay | CPU Limit | Memory Limit | GPU |
|---------|-----------|--------------|-----|
| Base | 2.0 | 1GB | No |
| Audio Proxy | 2.0 | 1.5GB | No |
| GPU | 4.0 | 2GB | Yes |
| Production | 4.0 | 2GB | Optional |

## Cleanup

```bash
# Stop and remove containers
docker compose down

# Remove volumes too (data loss!)
docker compose down -v

# Remove images
docker compose down --rmi all
```

## Troubleshooting

### Issue: Port already in use
```bash
# Check what's using port 8080
ss -tlnp | grep 8080

# Use different port in .env
echo "XG2G_LISTEN=:8081" >> .env
```

### Issue: GPU not detected
```bash
# Verify NVIDIA runtime
docker run --rm --gpus all nvidia/cuda:11.0-base nvidia-smi

# Check docker daemon.json
cat /etc/docker/daemon.json
# Should contain: "default-runtime": "nvidia"
```

### Issue: Monitoring stack fails
```bash
# Check Prometheus config
docker compose -f docker-compose.yml -f docker-compose.monitoring.yml config

# Validate syntax
yamllint docker-compose.monitoring.yml
```

## Migration from Old Structure

If you have separate compose files, migrate using overlays:

```bash
# Old way (9 separate files)
docker compose -f docker-compose.gpu.yml up

# New way (base + overlay)
docker compose -f docker-compose.yml -f docker-compose.gpu.yml up
```

## Advanced: Custom Overlays

Create your own overlay:

```yaml
# docker-compose.custom.yml
services:
  xg2g:
    environment:
      - XG2G_CUSTOM_FEATURE=enabled
    ports:
      - "8888:8080"  # Custom port mapping
```

Use it:
```bash
docker compose -f docker-compose.yml -f docker-compose.custom.yml up -d
```

## CI/CD Integration

### GitHub Actions Example
```yaml
- name: Start xg2g stack
  run: |
    docker compose \
      -f docker-compose.yml \
      -f docker-compose.minimal.yml \
      up -d

- name: Wait for health
  run: |
    timeout 30 sh -c 'until curl -f http://localhost:8080/healthz; do sleep 1; done'
```

### GitLab CI Example
```yaml
services:
  - docker:dind

script:
  - docker compose -f docker-compose.yml -f docker-compose.test.yml up -d
  - docker compose exec -T xg2g /app/xg2g --version
```

## Related Documentation

- [Deployment Guide](deployment-guide.md)
- [Monitoring Setup](monitoring.md)
- [GPU Transcoding](../features/gpu-transcoding.md)
- [Configuration Reference](../guides/configuration.md)
