# Security Token Rotation Guide

## 🔐 API Token Rotation (Best Practice)

This document describes the process for rotating your xg2g API token.

**Important:** Never commit actual tokens to git. This is an example template.

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
XG2G_API_TOKEN=your-new-token-here \
curl -X POST http://localhost:8080/api/refresh \
  -H "X-API-Token: your-new-token-here"
```

**Kubernetes secret update:**
```bash
kubectl create secret generic xg2g-secrets \
  --from-literal=api-token=your-new-token-here \
  --dry-run=client -o yaml | kubectl apply -f -
```

---
**Remember to rotate tokens regularly** (recommended: monthly for production)
