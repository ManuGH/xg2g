# xg2g Production Operations Quick Reference

## 🚀 Go-Live (3 Steps)
```bash
# 1. Configure
cp .env.prod.template .env.prod
# Edit: XG2G_OWI_BASE, XG2G_BOUQUET, XG2G_API_TOKEN

# 2. Deploy  
./scripts/prod-deploy.sh

# 3. Verify
curl -sf http://localhost:8080/healthz
curl -sf -X POST http://localhost:8080/api/refresh -H "X-API-Token: $(grep ^XG2G_API_TOKEN .env.prod | cut -d= -f2)"
curl -sf http://localhost:9090/metrics | grep xg2g_channels
```

## 🛠️ Day-to-Day Operations
```bash
# Status check
./scripts/prod-ops.sh status

# Watch logs
./scripts/prod-ops.sh logs

# Restart services
./scripts/prod-ops.sh restart

# Manual refresh
curl -X POST http://localhost:8080/api/refresh -H "X-API-Token: YOUR_TOKEN"
```

## 🔄 Emergency Rollback
```bash
# Quick rollback
./scripts/prod-ops.sh rollback

# Full cleanup (interactive)
./scripts/prod-ops.sh cleanup
```

## 📊 Key Endpoints
- **Health**: `GET http://localhost:8080/healthz`
- **Ready**: `GET http://localhost:8080/readyz`
- **Status**: `GET http://localhost:8080/api/status`
- **Refresh**: `POST http://localhost:8080/api/refresh` (requires X-API-Token)
- **Metrics**: `GET http://localhost:9090/metrics`
- **Files**: `GET http://localhost:8080/files/playlist.m3u`

## 🚨 Key Metrics to Monitor
```promql
# Success rate
rate(xg2g_openwebif_request_success_total[5m])

# Failure rate  
rate(xg2g_openwebif_request_failures_total[5m])

# Current channel count
xg2g_channels

# Last successful refresh
xg2g_last_refresh_timestamp
```

## 🔧 Troubleshooting

| Issue | Check | Solution |
|-------|--------|----------|
| `/readyz` returns 503 | No successful refresh yet | Trigger manual refresh, check OWI connectivity |
| `channels: 0` | OpenWebIF connection | Verify XG2G_OWI_BASE and XG2G_BOUQUET in .env.prod |
| API 401 errors | Missing/wrong token | Check X-API-Token header matches .env.prod |
| Container won't start | Config errors | Check `docker logs xg2g` for startup errors |

## 📋 Configuration Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `XG2G_OWI_BASE` | ✅ | - | OpenWebIF base URL |
| `XG2G_BOUQUET` | ✅ | - | Bouquet name to process |
| `XG2G_API_TOKEN` | ✅ | - | API authentication token |
| `XG2G_DATA` | ❌ | ./data | Output directory |
| `XG2G_LISTEN` | ❌ | :8080 | HTTP listen address |
| `XG2G_METRICS_LISTEN` | ❌ | :9090 | Metrics listen address |

## 🎯 Success Criteria
- ✅ `/healthz` returns `{"status":"ok"}`
- ✅ `/readyz` returns `{"status":"ok"}` (after first refresh)
- ✅ `/api/status` shows `channels > 0`
- ✅ `xg2g_channels` metric > 0
- ✅ Files generated in `./data/` directory
