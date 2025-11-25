# xg2g Kubernetes Deployment

Production-ready Kubernetes manifests for xg2g with auto-scaling, load balancing, and high availability.

## Features

- **Auto-Scaling**: HorizontalPodAutoscaler with CPU, memory, and custom metrics
- **High Availability**: 3+ replicas with pod anti-affinity
- **Persistent Storage**: PersistentVolumeClaim for EPG data, M3U playlists, cache
- **Load Balancing**: NGINX Ingress with sticky sessions
- **Health Checks**: Liveness, readiness, and startup probes
- **Redis Cache**: Optional distributed caching for multi-replica deployments
- **Network Security**: NetworkPolicies for ingress/egress control
- **Security**: Non-root containers, read-only filesystem, security contexts
- **Monitoring**: Prometheus ServiceMonitor, custom alerts, Grafana dashboard
- **Automated Backups**: Daily CronJob for data backup

## Prerequisites

**Required:**
- Kubernetes 1.24+
- kubectl configured
- StorageClass available (for PersistentVolumes)
- NGINX Ingress Controller
- Metrics Server (for HPA)

**Optional:**
- Prometheus Operator (for ServiceMonitor and alerts)
- Redis (for distributed caching, included in manifests)
- Cert-Manager (for TLS certificates)

## Quick Start

### 1. Install Dependencies

```bash
# Install NGINX Ingress Controller
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/cloud/deploy.yaml

# Install Metrics Server
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml

# Install Prometheus Adapter (for custom metrics)
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm install prometheus-adapter prometheus-community/prometheus-adapter
```

### 2. Deploy Redis

```bash
# Using Helm
helm repo add bitnami https://charts.bitnami.com/bitnami
helm install redis bitnami/redis \
  --namespace xg2g \
  --create-namespace \
  --set auth.enabled=false \
  --set master.persistence.size=1Gi
```

### 3. Create Secrets

```bash
# Copy the example secrets file
cp secrets.yaml.example secrets.yaml

# Edit with your credentials
vim secrets.yaml

# Apply secrets
kubectl apply -f secrets.yaml
```

### 3. Create Secrets (REQUIRED)

```bash
# Copy the example secrets file
cp secrets.yaml.example secrets.yaml

# Edit with your credentials
nano secrets.yaml

# Create the secret
kubectl create -f secrets.yaml
```

### 4. Deploy xg2g

**Using Kustomize (Recommended):**
```bash
# Edit kustomization.yaml and uncomment resources as needed
# Uncomment secrets.yaml line after creating secrets

# Build and apply
kustomize build . | kubectl apply -f -

# Or with kubectl
kubectl apply -k .
```

**Using kubectl (Manual):**
```bash
# Core resources (required)
kubectl apply -f namespace.yaml
kubectl apply -f pvc.yaml          # Persistent storage
kubectl apply -f configmap.yaml
kubectl apply -f secrets.yaml       # Created in step 3
kubectl apply -f deployment.yaml
kubectl apply -f service.yaml
kubectl apply -f hpa.yaml
kubectl apply -f ingress.yaml
kubectl apply -f networkpolicy.yaml  # Network security

# Optional resources
kubectl apply -f redis.yaml          # Distributed cache
kubectl apply -f servicemonitor.yaml # Prometheus monitoring
kubectl apply -f backup-cronjob.yaml # Automated backups
```

## Configuration

### ConfigMap

Edit `configmap.yaml` to configure:
- OpenWebIF endpoint
- EPG settings
- Cache backend
- Logging level

### Secrets

Edit `secrets.yaml` (created from `secrets.yaml.example`):
- OpenWebIF credentials
- API token
- Redis password (if using authentication)

### Ingress

Edit `ingress.yaml` to configure:
- Domain name (replace `xg2g.example.com`)
- TLS certificate
- Rate limiting
- Timeouts

## Auto-Scaling

The HPA configuration scales based on:

1. **CPU**: Target 70% utilization
2. **Memory**: Target 80% utilization
3. **Active Streams**: Target 50 streams per pod
4. **HTTP Requests**: Target 100 req/s per pod
5. **GPU Queue Size**: Target 30 items per pod

Scaling behavior:
- **Min replicas**: 3
- **Max replicas**: 20
- **Scale up**: Immediate, up to 100% or 4 pods at once
- **Scale down**: After 5 minutes, max 50% or 2 pods at once

## Monitoring

### Prometheus Metrics

Metrics are exposed at `/metrics` on port 8080:

```bash
kubectl port-forward -n xg2g svc/xg2g 8080:8080
curl http://localhost:8080/metrics
```

### View Logs

```bash
# Stream logs from all pods
kubectl logs -n xg2g -l app=xg2g -f

# Logs from specific pod
kubectl logs -n xg2g <pod-name> -f
```

### Health Checks

```bash
# Check pod status
kubectl get pods -n xg2g

# Check HPA status
kubectl get hpa -n xg2g

# Describe HPA for detailed metrics
kubectl describe hpa -n xg2g xg2g
```

## Troubleshooting

### Pods not starting

```bash
# Check pod events
kubectl describe pod -n xg2g <pod-name>

# Check logs
kubectl logs -n xg2g <pod-name>

# Check resource quotas
kubectl describe resourcequota -n xg2g
```

### HPA not scaling

```bash
# Check if metrics are available
kubectl get hpa -n xg2g
kubectl describe hpa -n xg2g xg2g

# Check metrics server
kubectl top pods -n xg2g

# Check custom metrics
kubectl get --raw /apis/custom.metrics.k8s.io/v1beta1
```

### Ingress not working

```bash
# Check ingress status
kubectl get ingress -n xg2g
kubectl describe ingress -n xg2g xg2g

# Check nginx ingress controller logs
kubectl logs -n ingress-nginx -l app.kubernetes.io/name=ingress-nginx
```

## Updating

### Rolling Update

```bash
# Update image tag in kustomization.yaml or deployment.yaml
kubectl set image deployment/xg2g xg2g=ghcr.io/manugh/xg2g:v1.2.0 -n xg2g

# Check rollout status
kubectl rollout status deployment/xg2g -n xg2g

# View rollout history
kubectl rollout history deployment/xg2g -n xg2g
```

### Rollback

```bash
# Rollback to previous version
kubectl rollout undo deployment/xg2g -n xg2g

# Rollback to specific revision
kubectl rollout undo deployment/xg2g --to-revision=2 -n xg2g
```

## Resource Management

### PodDisruptionBudget

Ensures at least 2 pods are always running during:
- Node maintenance
- Cluster upgrades
- Voluntary disruptions

### ResourceQuota

Namespace limits:
- CPU requests: 20 cores
- Memory requests: 10Gi
- CPU limits: 40 cores
- Memory limits: 20Gi
- Max pods: 50

### LimitRange

Default container limits:
- CPU: 250m request, 1000m limit
- Memory: 256Mi request, 512Mi limit

## Security

### Pod Security

- **Non-root user**: Runs as UID 65532
- **Read-only root filesystem**: Prevents container modifications
- **Dropped capabilities**: ALL capabilities dropped except NET_BIND_SERVICE
- **No privilege escalation**: Prevents privilege escalation attacks

### Network Policies

Consider adding NetworkPolicies to restrict traffic:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: xg2g-netpol
  namespace: xg2g
spec:
  podSelector:
    matchLabels:
      app: xg2g
  policyTypes:
    - Ingress
    - Egress
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              name: ingress-nginx
  egress:
    - to:
        - namespaceSelector:
            matchLabels:
              name: xg2g
      ports:
        - protocol: TCP
          port: 6379  # Redis
```

## High Availability Checklist

- [x] Multiple replicas (3+)
- [x] Pod anti-affinity (spread across nodes)
- [x] PodDisruptionBudget (min 2 available)
- [x] Health checks (liveness, readiness, startup)
- [x] Resource limits (prevents OOM kills)
- [x] Graceful shutdown (30s termination grace)
- [x] Rolling updates (zero downtime)
- [x] Auto-scaling (HPA with multiple metrics)
- [x] Sticky sessions (for streaming continuity)

## Performance Tuning

### Increase replicas

```bash
kubectl scale deployment xg2g --replicas=10 -n xg2g
```

### Adjust HPA thresholds

Edit `hpa.yaml` and adjust target utilization values.

### Tune NGINX buffers

Edit ingress annotations for larger streams:

```yaml
nginx.ingress.kubernetes.io/proxy-buffer-size: "128k"
nginx.ingress.kubernetes.io/proxy-buffers-number: "4"
```

## License

MIT

## Persistent Storage

The deployment uses PersistentVolumeClaims for data persistence:

### Main Data PVC (`xg2g-data`)
- **Size**: 10Gi (adjust based on your needs)
- **Access Mode**: ReadWriteOnce
- **Stores**: M3U playlists, XMLTV EPG data, cache files

### Redis Data PVC (`redis-data`)
- **Size**: 5Gi
- **Access Mode**: ReadWriteOnce
- **Stores**: Redis persistence (RDB + AOF)

### Backup PVC (`xg2g-backup`)
- **Size**: 50Gi (should be >= data PVC size)
- **Access Mode**: ReadWriteOnce
- **Stores**: Automated backups (7-day retention)

**⚠️ Important**: Without persistent storage, all data (EPG, cache) will be lost on pod restart!

## Network Security

NetworkPolicies restrict traffic to/from xg2g pods:

**Allowed Ingress:**
- Ingress Controller → xg2g:8080 (HTTP API)
- Same namespace → xg2g:8080 (internal services)
- Any → xg2g:1900 (SSDP discovery, UDP)

**Allowed Egress:**
- xg2g → kube-dns:53 (DNS resolution)
- xg2g → External:80/443/8001 (OpenWebIF receiver)
- xg2g → redis:6379 (cache)
- xg2g → monitoring:9090 (Prometheus)

**⚠️ Note**: Adjust the NetworkPolicy for your environment (e.g., ingress controller namespace).

## Backup & Restore

### Automated Backups

The backup CronJob runs daily at 2 AM:

```bash
# Check backup jobs
kubectl get cronjobs -n xg2g
kubectl get jobs -n xg2g

# View backup logs
kubectl logs -n xg2g -l app=xg2g-backup
```

### Manual Backup

```bash
# Create one-time backup
kubectl create job -n xg2g --from=cronjob/xg2g-backup manual-backup-$(date +%s)

# Download backup to local
kubectl exec -n xg2g deployment/xg2g -- tar -czf - -C /data . > xg2g-backup-$(date +%Y%m%d).tar.gz
```

### Restore from Backup

```bash
# Scale down deployment
kubectl scale deployment/xg2g -n xg2g --replicas=0

# Restore data
kubectl cp xg2g-backup-20250112.tar.gz xg2g/<pod-name>:/tmp/backup.tar.gz -n xg2g
kubectl exec -n xg2g <pod-name> -- tar -xzf /tmp/backup.tar.gz -C /data

# Scale back up
kubectl scale deployment/xg2g -n xg2g --replicas=3
```

## Monitoring with Prometheus

### Setup ServiceMonitor

If using Prometheus Operator:

```bash
# Apply ServiceMonitor
kubectl apply -f servicemonitor.yaml

# Verify Prometheus is scraping
kubectl port-forward -n monitoring svc/prometheus 9090:9090
# Open http://localhost:9090/targets
```

### Pre-configured Alerts

The ServiceMonitor includes alerts for:
- **XG2GHighErrorRate**: >5% HTTP 5xx errors for 5 minutes
- **XG2GDown**: Service unreachable for 2 minutes
- **XG2GHighMemory**: >90% memory usage for 5 minutes
- **XG2GCircuitBreakerOpen**: Circuit breaker open for 5 minutes
- **XG2GHighGPUQueue**: GPU queue >10 items for 10 minutes
- **XG2GEPGSyncFailed**: EPG not updated in 24 hours

### Grafana Dashboard

Import the included dashboard:

```bash
kubectl get configmap -n xg2g xg2g-dashboard -o jsonpath='{.data.xg2g-dashboard\.json}' | \
  curl -X POST -H "Content-Type: application/json" \
  -d @- http://grafana.example.com/api/dashboards/db
```

## Redis Distributed Cache

For multi-replica deployments, Redis provides shared caching:

**Benefits:**
- ✅ Shared EPG cache across all pods
- ✅ Consistent channel mapping
- ✅ Reduced load on OpenWebIF receiver
- ✅ Faster response times

**When to use:**
- Running 3+ replicas
- High request rate (>100 req/s)
- Large EPG data (>100 channels)

**When NOT needed:**
- Single replica (local cache sufficient)
- Low traffic (<50 req/s)
- Small channel lists (<50 channels)

## Troubleshooting

### Pod not starting

```bash
# Check pod status
kubectl get pods -n xg2g
kubectl describe pod -n xg2g <pod-name>

# View logs
kubectl logs -n xg2g <pod-name> --previous

# Common issues:
# - PVC not bound (check: kubectl get pvc -n xg2g)
# - Image pull failed (check: kubectl describe pod)
# - Secrets missing (check: kubectl get secrets -n xg2g)
```

### Storage issues

```bash
# Check PVC binding
kubectl get pvc -n xg2g

# Check PV
kubectl get pv

# Resize PVC (if StorageClass supports it)
kubectl patch pvc xg2g-data -n xg2g -p '{"spec":{"resources":{"requests":{"storage":"20Gi"}}}}'
```

### High memory usage

```bash
# Check memory metrics
kubectl top pods -n xg2g

# Adjust limits in deployment.yaml
# Or increase HPA memory target
```

### Circuit breaker frequently opening

```bash
# Check receiver connectivity
kubectl exec -n xg2g deployment/xg2g -- wget -O- http://receiver:80/api/statusinfo

# Increase timeout/retry in configmap.yaml
# Monitor: kubectl logs -n xg2g -l app=xg2g | grep circuit
```

## Production Checklist

Before deploying to production:

- [ ] **Secrets**: Created from secrets.yaml.example with real credentials
- [ ] **Storage**: PVCs created and bound to available PVs
- [ ] **Ingress**: Domain name configured, TLS certificate installed
- [ ] **Resource Limits**: Adjusted based on load testing
- [ ] **HPA**: Metrics server installed and working
- [ ] **Monitoring**: Prometheus/Grafana configured (optional but recommended)
- [ ] **Backups**: Backup CronJob configured with external storage (S3/GCS/Azure)
- [ ] **NetworkPolicy**: Adjusted for your ingress controller and environment
- [ ] **Health Checks**: Tested with `kubectl get pods` (all should be Ready)
- [ ] **Load Testing**: Run `test/loadtest/loadtest-k6.js` to verify scaling
- [ ] **Documentation**: Updated ConfigMap with production receiver URL

## Scaling Strategy

### Horizontal Scaling

HPA automatically scales based on metrics. To adjust:

```yaml
# In hpa.yaml
spec:
  minReplicas: 5        # Increase for higher baseline
  maxReplicas: 50       # Increase for burst capacity
```

### Vertical Scaling

Adjust resource limits in deployment.yaml:

```yaml
resources:
  requests:
    cpu: 500m           # Increase for CPU-heavy workloads
    memory: 512Mi       # Increase for large EPG data
  limits:
    cpu: 2000m          # Increase for transcoding
    memory: 1Gi         # Increase for caching
```

## Security Best Practices

1. **Secrets**: Never commit secrets.yaml to git (it's in .gitignore)
2. **RBAC**: Use ServiceAccount with minimal permissions
3. **NetworkPolicy**: Restrict traffic to necessary ports only
4. **TLS**: Always use TLS for ingress (configure cert-manager)
5. **Image**: Use specific image tags, not `:latest` in production
6. **Security Scanning**: Enable PodSecurityPolicies or PSA
7. **Audit**: Enable Kubernetes audit logging
8. **Updates**: Regular security updates for base images

## Support

- **Issues**: https://github.com/ManuGH/xg2g/issues
- **Discussions**: https://github.com/ManuGH/xg2g/discussions
- **Documentation**: https://github.com/ManuGH/xg2g/tree/main/docs
