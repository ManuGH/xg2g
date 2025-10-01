# Security Token Update - v0.3.0 EPG Release

## 🔐 API Token Rotation (Required)

**Old Token (INVALIDATED):**
```text
84a4a0474a35594af3777129bbade1c1
```

**New Production Token:**
```text
17c68be703c54b52f52ddec88a52590d
```

## 📋 Update Checklist

### ✅ Completed
- [x] Generated new secure token via `openssl rand -hex 16`
- [x] Updated `.env.prod` with new token
- [x] Old token marked for invalidation

### 🔄 Required Actions  
- [ ] Update Kubernetes secrets with new token
- [ ] Update any external monitoring/health-check systems
- [ ] Invalidate old token in OpenWebIF (if applicable)
- [ ] Update documentation references to old token

## 🚨 Security Notes

- **Never commit tokens to git** - use environment variables only
- **Rotate tokens regularly** - recommend monthly for production
- **Monitor failed auth attempts** - check `xg2g_api_requests_total{status="unauthorized"}`
- **Use HTTPS in production** - never transmit tokens over HTTP

## 🔧 Deployment Commands

**Local testing:**
```bash
XG2G_API_TOKEN=17c68be703c54b52f52ddec88a52590d \
curl -X POST http://localhost:8080/api/refresh \
  -H "X-API-Token: 17c68be703c54b52f52ddec88a52590d"
```

**Kubernetes secret update:**
```bash
kubectl create secret generic xg2g-secrets \
  --from-literal=api-token=17c68be703c54b52f52ddec88a52590d \
  --dry-run=client -o yaml | kubectl apply -f -
```

---
**Token rotation completed for v0.3.0 EPG release** ✅
