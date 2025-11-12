# xg2g Kubernetes Deployment

Production-ready Kubernetes manifests for xg2g with auto-scaling, load balancing, and high availability.

## Features

- **Auto-Scaling**: HorizontalPodAutoscaler with CPU, memory, and custom metrics
- **High Availability**: 3+ replicas with pod anti-affinity
- **Load Balancing**: NGINX Ingress with sticky sessions
- **Health Checks**: Liveness, readiness, and startup probes
- **Redis Cache**: Distributed caching for EPG and channel data
- **Security**: Non-root containers, read-only filesystem, security contexts
- **Monitoring**: Prometheus metrics, Jaeger tracing

## Prerequisites

- Kubernetes 1.24+
- kubectl configured
- NGINX Ingress Controller
- Metrics Server (for HPA)
- Prometheus Adapter (for custom metrics)
- Redis (can be deployed with Helm)

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

### 4. Deploy xg2g

Using kubectl:
```bash
# Apply all manifests
kubectl apply -f namespace.yaml
kubectl apply -f configmap.yaml
kubectl apply -f secrets.yaml
kubectl apply -f deployment.yaml
kubectl apply -f service.yaml
kubectl apply -f hpa.yaml
kubectl apply -f ingress.yaml
```

Using Kustomize:
```bash
# Build and apply
kustomize build . | kubectl apply -f -

# Or with kubectl
kubectl apply -k .
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
