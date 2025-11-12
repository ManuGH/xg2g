# Incident Response & Rollback Runbook

**Target Version:** v1.7.x
**Last Updated:** 2025-11-01
**Owner:** Release Engineering

---

## 🚨 Incident Severity Levels

| Level | Description | Response Time | Escalation |
|-------|-------------|---------------|------------|
| **P0** | Complete service outage | < 5 min | Immediate rollback |
| **P1** | Degraded service (>50% errors) | < 15 min | Rollback if unfixable in 30 min |
| **P2** | Partial degradation (<50% errors) | < 1 hour | Hotfix or rollback |
| **P3** | Minor issues, workaround available | < 4 hours | Scheduled hotfix |

---

## 📊 Health Check Indicators

### Critical Metrics (P0 Triggers)

```bash
# Check service health
curl http://localhost:8080/healthz
# Expected: {"status":"healthy",...}

# Check readiness
curl http://localhost:8080/readyz
# Expected: {"ready":true,...}

# Check Prometheus metrics
curl http://localhost:9090/metrics | grep -E 'xg2g_(up|ready)'
# Expected: xg2g_up 1, xg2g_ready 1
```

**P0 Triggers:**
- `/healthz` returns non-200
- `/readyz` returns 503 for >2 minutes
- Error rate >25% for >5 minutes
- No successful refresh in >6 hours

### Degraded Service Indicators (P1)

**Prometheus Queries:**
```promql
# Error rate spike
rate(xg2g_api_requests_total{status=~"5.."}[5m]) > 0.1

# Cache hit rate drop
rate(xg2g_cache_hits_total[5m]) / rate(xg2g_cache_requests_total[5m]) < 0.5

# Refresh failures
increase(xg2g_refresh_errors_total[1h]) > 3

# Response time degradation
histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m])) > 2.0
```

---

## 🔄 Rollback Procedures

### Option 1: Kubernetes Rolling Rollback

**Use when:** Pod-level issues, immediate rollback needed

```bash
# Check current deployment
kubectl get deploy xg2g -o jsonpath='{.spec.template.spec.containers[0].image}'

# Rollback to previous version
kubectl rollout undo deployment/xg2g

# Monitor rollback progress
kubectl rollout status deployment/xg2g

# Verify health
kubectl exec -it deploy/xg2g -- wget -qO- http://localhost:8080/healthz
```

**Expected Duration:** 2-5 minutes
**Impact:** Zero-downtime if readiness probes configured

### Option 2: Helm Rollback

**Use when:** Configuration changes included in release

```bash
# List release history
helm history xg2g

# Rollback to previous revision
helm rollback xg2g

# Verify rollback
helm status xg2g
kubectl get pods -l app=xg2g
```

**Expected Duration:** 3-7 minutes
**Impact:** Brief disruption during ConfigMap reload

### Option 3: Docker Image Revert

**Use when:** Single container deployment

```bash
# Stop current container
docker stop xg2g
docker rm xg2g

# Pull previous stable version
docker pull ghcr.io/manugh/xg2g:1.6.1

# Start with previous image
docker run -d --name xg2g \
  -v xg2g-data:/data \
  -p 8080:8080 \
  -p 9090:9090 \
  --env-file /etc/xg2g/.env \
  ghcr.io/manugh/xg2g:1.6.1

# Verify health
docker logs xg2g --tail 50
curl http://localhost:8080/healthz
```

**Expected Duration:** 1-3 minutes
**Impact:** Full downtime during container restart

### Option 4: Git Revert + Redeploy

**Use when:** Code-level issue requiring immediate patch

```bash
# Revert problematic commit
git revert <commit-sha>
git push origin main

# Trigger CI/CD pipeline or manual deploy
kubectl set image deployment/xg2g xg2g=ghcr.io/manugh/xg2g:v1.6.1
```

**Expected Duration:** 5-15 minutes (depends on CI/CD)
**Impact:** Controlled rollout via deployment strategy

---

## 🛠️ Incident Response Workflow

### Step 1: Detect & Assess (< 5 min)

1. **Check Alerts:**
   ```bash
   # Prometheus Alertmanager
   curl http://alertmanager:9093/api/v2/alerts

   # Grafana dashboards
   open https://grafana.example.com/d/xg2g-overview
   ```

2. **Verify Incident:**
   ```bash
   # Quick health check
   kubectl exec -it deploy/xg2g -- sh -c '
     echo "=== Health ==="
     wget -qO- http://localhost:8080/healthz?verbose=true | jq
     echo "=== Readiness ==="
     wget -qO- http://localhost:8080/readyz?verbose=true | jq
     echo "=== Recent Logs ==="
     tail -50 /var/log/xg2g/app.log | grep -E "ERROR|FATAL"
   '
   ```

3. **Determine Severity:**
   - P0: Immediate rollback
   - P1: 30-min fix window
   - P2/P3: Schedule hotfix

### Step 2: Communicate (< 2 min)

```bash
# Post to incident channel
cat << MSG
🚨 INCIDENT DECLARED: P0 - xg2g Service Outage

**Detected:** $(date -Iseconds)
**Symptoms:** /healthz returning 503, error rate 45%
**Impact:** Full service unavailable
**Action:** Initiating rollback to v1.6.1
**ETA:** 5 minutes
**Status Page:** https://status.example.com

Updates every 5 minutes.
MSG
```

### Step 3: Execute Rollback (< 5 min)

**Use Decision Tree:**

```
Is it a K8s deployment?
├─ Yes → kubectl rollout undo (2-5 min)
└─ No  → Is Helm used?
        ├─ Yes → helm rollback (3-7 min)
        └─ No  → Docker revert (1-3 min)
```

**Execute chosen method from "Rollback Procedures" above.**

### Step 4: Verify Recovery (< 5 min)

```bash
# Health checks
for i in {1..10}; do
  curl -s http://localhost:8080/healthz | jq -r '.status'
  sleep 2
done
# Expected: 10x "healthy"

# Metrics verification
curl -s http://localhost:9090/metrics | grep -E 'xg2g_(up|ready|errors_total)'

# Check recent logs for errors
kubectl logs -l app=xg2g --tail=100 | grep -E 'ERROR|FATAL' || echo "No errors"
```

**Success Criteria:**
- `/healthz` → 200 OK for 2 minutes
- `/readyz` → 200 OK for 2 minutes
- Error rate < 1% for 5 minutes
- Cache hit rate > 70%

### Step 5: Post-Incident (< 30 min)

1. **Update Status Page:**
   ```
   ✅ RESOLVED: xg2g Service Restored

   Rollback to v1.6.1 completed at [TIME].
   All health checks passing.
   Post-mortem scheduled for [DATE].
   ```

2. **Capture Evidence:**
   ```bash
   # Save logs
   kubectl logs -l app=xg2g --since=1h > incident-logs-$(date +%Y%m%d-%H%M).log

   # Export metrics
   curl "http://prometheus:9090/api/v1/query_range?query=xg2g_up&start=$(date -u -d '1 hour ago' +%s)&end=$(date +%s)&step=15s" \
     > incident-metrics-$(date +%Y%m%d-%H%M).json
   ```

3. **Schedule Post-Mortem:**
   - Within 24 hours for P0/P1
   - Within 1 week for P2/P3
   - Template: 5 Whys, Timeline, Action Items

---

## 🧪 Common Issues & Quick Fixes

### Issue: High Memory Usage

**Symptoms:**
```bash
kubectl top pods -l app=xg2g
# Shows >1GB memory usage
```

**Quick Fix:**
```bash
# Restart pods with memory limit
kubectl set resources deployment/xg2g --limits=memory=512Mi --requests=memory=256Mi
kubectl rollout restart deployment/xg2g
```

### Issue: Cache Miss Rate Spike

**Symptoms:**
```bash
# Cache hit rate <50%
curl http://localhost:9090/metrics | grep xg2g_cache
```

**Quick Fix:**
```bash
# Increase cache TTL via config reload
kubectl exec -it deploy/xg2g -- sh -c '
  sed -i "s/cache_ttl: 5m/cache_ttl: 10m/" /etc/xg2g/config.yaml
  curl -X POST http://localhost:8080/api/v1/config/reload
'
```

### Issue: Receiver API Timeout

**Symptoms:**
```bash
# Logs show "context deadline exceeded"
kubectl logs -l app=xg2g | grep "deadline exceeded"
```

**Quick Fix:**
```bash
# Disable caching temporarily to diagnose
kubectl set env deployment/xg2g XG2G_CACHE_ENABLED=false
kubectl rollout restart deployment/xg2g
```

### Issue: SSDP Announcer Crash Loop

**Symptoms:**
```bash
# Logs show "bind: address already in use"
kubectl logs -l app=xg2g | grep "bind.*1900"
```

**Quick Fix:**
```bash
# Disable SSDP temporarily
kubectl set env deployment/xg2g XG2G_HDHR_ENABLED=false
kubectl rollout restart deployment/xg2g
```

---

## 📞 Escalation Contacts

| Role | Contact | Availability |
|------|---------|--------------|
| On-Call Engineer | Slack: @oncall | 24/7 |
| Release Manager | @ManuGH | Business hours |
| Infrastructure | ops-team@example.com | 24/7 |
| Security | security@example.com | 24/7 |

**Escalation Path:**
1. On-Call Engineer (immediate)
2. Release Manager (if rollback fails)
3. Infrastructure Team (if infrastructure issue)

---

## 🔍 Troubleshooting Commands Cheat Sheet

```bash
# === Health & Status ===
curl http://localhost:8080/healthz?verbose=true
curl http://localhost:8080/readyz?verbose=true
kubectl get pods -l app=xg2g -o wide

# === Logs ===
kubectl logs -l app=xg2g --tail=100 -f
kubectl logs -l app=xg2g --previous  # Previous pod instance
kubectl logs -l app=xg2g --since=1h | grep ERROR

# === Metrics ===
curl http://localhost:9090/metrics | grep xg2g_
kubectl top pods -l app=xg2g

# === Networking ===
kubectl exec -it deploy/xg2g -- netstat -tuln
kubectl exec -it deploy/xg2g -- nslookup receiver.local

# === Configuration ===
kubectl exec -it deploy/xg2g -- cat /etc/xg2g/config.yaml
kubectl get configmap xg2g-config -o yaml

# === Container Info ===
kubectl describe pod -l app=xg2g
docker inspect xg2g  # For Docker deployments
```

---

## 📝 Post-Mortem Template

```markdown
# Post-Mortem: xg2g v1.7.x Incident

**Date:** [YYYY-MM-DD]
**Severity:** P0/P1/P2/P3
**Duration:** [X minutes/hours]
**Impact:** [User-facing impact]

## Timeline

- [HH:MM] - Incident detected
- [HH:MM] - Rollback initiated
- [HH:MM] - Service restored

## Root Cause

[5 Whys analysis]

## What Went Well

- Fast detection (<5 min)
- Rollback procedure worked as documented

## What Went Wrong

- [Issues encountered]

## Action Items

- [ ] [Action 1] - Owner: @user - Due: YYYY-MM-DD
- [ ] [Action 2] - Owner: @user - Due: YYYY-MM-DD

## Lessons Learned

[Key takeaways]
```

---

**Last Tested:** [Run periodic rollback drills quarterly]
**Next Review:** [Schedule after each P0/P1 incident]
