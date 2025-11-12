# Permissions Documentation

This document describes all permissions required by xg2g for different deployment scenarios.

## Table of Contents

- [Docker Container Permissions](#docker-container-permissions)
- [GitHub Actions Workflow Permissions](#github-actions-workflow-permissions)
- [Kubernetes RBAC Permissions](#kubernetes-rbac-permissions)
- [File System Permissions](#file-system-permissions)
- [Network Permissions](#network-permissions)
- [Device Permissions](#device-permissions)

---

## Docker Container Permissions

### Standard Mode (Non-Root Container)

xg2g runs as a non-root user by default for security:

```yaml
# Container runs as user xg2g (UID: 65532)
USER: xg2g:xg2g
```

**Security Context:**
- `runAsNonRoot: true` - Container does not run as root
- `runAsUser: 65532` - Runs as non-privileged user
- `runAsGroup: 65532` - Runs with non-privileged group
- `readOnlyRootFilesystem: true` - Root filesystem is read-only
- `allowPrivilegeEscalation: false` - Cannot escalate privileges

**Capabilities:**
- **Dropped:** ALL (drops all Linux capabilities)
- **Added:** NET_BIND_SERVICE (only for binding to port 8080)

### Volume Permissions

When using bind mounts, ensure the host directories are readable/writable by UID 65532:

```bash
# Create directory with correct ownership
mkdir -p /path/to/data
chown 65532:65532 /path/to/data
chmod 755 /path/to/data
```

Or run as root in the container (not recommended):

```yaml
services:
  xg2g:
    user: "0:0"  # Run as root (security risk)
```

### GPU Transcoding Mode

For GPU-accelerated transcoding, the container needs access to GPU devices:

**Required Device Access:**
```yaml
devices:
  - /dev/dri:/dev/dri  # Direct Rendering Infrastructure
```

**User Permissions:**
The container user (65532) must be in the `video` or `render` group on the host:

```bash
# Check GPU device permissions
ls -l /dev/dri/renderD128
# crw-rw---- 1 root video 226, 128 Nov 12 12:00 /dev/dri/renderD128

# Option 1: Add user to video group on host
sudo usermod -aG video $(whoami)

# Option 2: Run container with host video group
docker run --group-add $(getent group video | cut -d: -f3) ...
```

**Docker Compose Example:**
```yaml
services:
  xg2g-gpu:
    image: ghcr.io/manugh/xg2g:latest
    devices:
      - /dev/dri:/dev/dri
    group_add:
      - video  # Add container user to video group
    environment:
      - XG2G_ENABLE_GPU_TRANSCODING=true
```

---

## GitHub Actions Workflow Permissions

xg2g uses minimal required permissions for CI/CD workflows following the principle of least privilege.

### Standard CI Workflow (`ci.yml`)

```yaml
permissions:
  contents: read  # Read repository content only
```

**Usage:** Building, testing, linting

### Docker Image Publishing (`docker.yml`, `docker-multi-cpu.yml`)

```yaml
permissions:
  contents: read       # Read repository content
  packages: write      # Push to GitHub Container Registry
  id-token: write      # OIDC token for Cosign signing
```

**Usage:** Build and publish Docker images with cryptographic signatures

### Security Scanning (`codeql.yml`, `gosec.yml`, `govulncheck.yml`)

```yaml
permissions:
  contents: read           # Read repository content
  security-events: write   # Upload security scan results
  actions: read           # Read workflow metadata
```

**Usage:** CodeQL analysis, security vulnerability scanning

### Container Security (`container-security.yml`)

```yaml
permissions:
  contents: read           # Global read-only access
  
  # Per-job permissions
  scan-job:
    permissions:
      security-events: write   # Upload Trivy scan results
      
  sbom-job:
    permissions:
      contents: write          # Upload SBOM artifacts
      packages: write          # Attach attestations to images
      id-token: write          # Sign with OIDC
```

**Usage:** Trivy vulnerability scanning, SBOM generation, provenance attestation

### Documentation Publishing (`docs.yml`)

```yaml
permissions:
  contents: read       # Read repository content
  pages: write        # Deploy to GitHub Pages
  id-token: write     # Verify Pages deployment
```

**Usage:** Build and publish API documentation

### Coverage Reports (`coverage.yml`)

```yaml
permissions:
  contents: write      # Commit coverage reports
  pages: write        # Deploy coverage to GitHub Pages
  id-token: write     # Verify deployment
```

**Usage:** Generate and publish code coverage reports

### Release Workflow (`release.yml`)

```yaml
permissions:
  contents: write      # Create releases and tags
  packages: write      # Push release images
  id-token: write      # Sign releases
```

**Usage:** Create GitHub releases, publish multi-arch images

### Benchmarking (`benchmark.yml`)

```yaml
permissions:
  contents: write           # Commit benchmark results
  pull-requests: write      # Comment on PRs
```

**Usage:** Run performance benchmarks, post results to PRs

### Scorecard Security Assessment (`scorecard.yml`)

```yaml
permissions: read-all  # Read-only access to all

jobs:
  scorecard:
    permissions:
      security-events: write   # Upload Scorecard results
      id-token: write          # Publish results
      contents: read           # Read repository
```

**Usage:** OpenSSF Scorecard security posture assessment

---

## Kubernetes RBAC Permissions

### Service Account

xg2g uses a dedicated Kubernetes ServiceAccount:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: xg2g
  namespace: xg2g
```

**Default Permissions:**
- No additional RBAC roles are required for basic operation
- The ServiceAccount is used for pod identity only

### Required Kubernetes Permissions (for deployment)

To deploy xg2g to Kubernetes, the user/system needs:

```yaml
# Namespace-level permissions
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: xg2g-deployer
  namespace: xg2g
rules:
  # Core resources
  - apiGroups: [""]
    resources: ["configmaps", "secrets", "services", "persistentvolumeclaims"]
    verbs: ["create", "update", "patch", "get", "list"]
  
  # Workload resources
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["create", "update", "patch", "get", "list"]
  
  # Networking
  - apiGroups: ["networking.k8s.io"]
    resources: ["ingresses"]
    verbs: ["create", "update", "patch", "get", "list"]
  
  # Monitoring (optional)
  - apiGroups: ["monitoring.coreos.com"]
    resources: ["servicemonitors"]
    verbs: ["create", "update", "patch", "get", "list"]
```

### Cluster-Level Permissions (optional)

For cluster-wide monitoring or mesh integration:

```yaml
# Only needed if using cluster-scoped ServiceMonitor
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: xg2g-metrics-reader
rules:
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list"]
```

### Pod Security Standards

xg2g complies with the **Restricted** Pod Security Standard:

```yaml
# Namespace label for Pod Security Admission
apiVersion: v1
kind: Namespace
metadata:
  name: xg2g
  labels:
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/audit: restricted
    pod-security.kubernetes.io/warn: restricted
```

**Security Policies Enforced:**
- ✅ Non-root user (UID 65532)
- ✅ Read-only root filesystem
- ✅ No privilege escalation
- ✅ Drop all capabilities (except NET_BIND_SERVICE)
- ✅ No host namespace access
- ✅ seccomp profile: RuntimeDefault

---

## File System Permissions

### Container File System

**Read-Only Paths:**
```
/app/               # Application binary (read-only)
/app/lib/           # Rust transcoder library (read-only)
/etc/               # System configuration (read-only)
```

**Writable Paths:**
```
/data/              # Persistent data (mounted volume)
/tmp/               # Temporary files (ephemeral)
```

### Host Volume Mounts

When mounting host directories:

```yaml
volumes:
  - type: bind
    source: /host/path/data
    target: /data
```

**Required Permissions:**
- **Owner:** UID 65532 (xg2g user)
- **Group:** GID 65532 (xg2g group)
- **Mode:** 755 (rwxr-xr-x) for directories
- **Mode:** 644 (rw-r--r--) for files

**Setup Script:**
```bash
#!/bin/bash
# Create directory with correct ownership
sudo mkdir -p /opt/xg2g/data
sudo chown 65532:65532 /opt/xg2g/data
sudo chmod 755 /opt/xg2g/data
```

### Kubernetes Persistent Volumes

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: xg2g-data
spec:
  accessModes:
    - ReadWriteOnce  # Single node read-write
  resources:
    requests:
      storage: 10Gi
  storageClassName: standard  # Or your storage class
```

**fsGroup in Pod Spec:**
```yaml
securityContext:
  fsGroup: 65532  # All volumes owned by group 65532
```

This ensures mounted volumes are accessible by the non-root user.

---

## Network Permissions

### Required Network Access

**Outbound (from xg2g container):**

| Destination | Port | Protocol | Purpose | Required |
|-------------|------|----------|---------|----------|
| Enigma2 Receiver | 80/443 | HTTP/HTTPS | OpenWebIF API | ✅ Yes |
| Enigma2 Receiver | 8001 | HTTP | Stream proxy source | Optional |
| Redis | 6379 | TCP | Cache backend | Optional |
| Jaeger | 14268 | HTTP | Distributed tracing | Optional |
| Prometheus | - | - | Metrics scraping (pull) | Optional |

**Inbound (to xg2g container):**

| Port | Protocol | Purpose | Bind Address | Required |
|------|----------|---------|--------------|----------|
| 8080 | TCP | HTTP API, M3U playlist, metrics | 0.0.0.0 | ✅ Yes |
| 18000 | TCP | Stream proxy (audio transcoding) | 0.0.0.0 | Optional |
| 1900 | UDP | SSDP discovery (HDHomeRun) | 239.255.255.250 (multicast) | Optional |

### Firewall Rules

**Docker Host:**
```bash
# Allow API access
sudo ufw allow 8080/tcp comment 'xg2g API'

# Allow stream proxy (if enabled)
sudo ufw allow 18000/tcp comment 'xg2g Stream Proxy'

# Allow SSDP multicast (HDHomeRun discovery)
sudo ufw allow 1900/udp comment 'xg2g SSDP Discovery'
```

**Kubernetes NetworkPolicy Example:**
```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: xg2g-network-policy
  namespace: xg2g
spec:
  podSelector:
    matchLabels:
      app: xg2g
  policyTypes:
    - Ingress
    - Egress
  
  ingress:
    # Allow traffic from ingress controller
    - from:
        - namespaceSelector:
            matchLabels:
              name: ingress-nginx
      ports:
        - protocol: TCP
          port: 8080
  
  egress:
    # Allow DNS
    - to:
        - namespaceSelector: {}
          podSelector:
            matchLabels:
              k8s-app: kube-dns
      ports:
        - protocol: UDP
          port: 53
    
    # Allow OpenWebIF access (adjust IP range)
    - to:
        - ipBlock:
            cidr: 192.168.0.0/16  # Your local network
      ports:
        - protocol: TCP
          port: 80
        - protocol: TCP
          port: 443
        - protocol: TCP
          port: 8001
    
    # Allow Redis access (if using)
    - to:
        - podSelector:
            matchLabels:
              app: redis
      ports:
        - protocol: TCP
          port: 6379
```

### SELinux / AppArmor

**SELinux (RHEL/Fedora):**
```bash
# Check if SELinux is enforcing
getenforce

# Allow container to bind to non-standard ports
sudo semanage port -a -t http_port_t -p tcp 8080
sudo semanage port -a -t http_port_t -p tcp 18000

# Label volume directories
sudo chcon -Rt svirt_sandbox_file_t /opt/xg2g/data
```

**AppArmor (Ubuntu/Debian):**
```bash
# Docker automatically applies a default AppArmor profile
# Custom profile usually not needed for xg2g

# Check if AppArmor is enabled
sudo aa-status | grep docker
```

---

## Device Permissions

### GPU Access (Hardware Transcoding)

**Required Devices:**
```
/dev/dri/renderD128    # GPU rendering device (primary)
/dev/dri/card0         # GPU card (optional, for VA-API)
```

**Device Permissions:**
```bash
# Check device ownership
ls -l /dev/dri/
# crw-rw---- 1 root video  226, 0 Nov 12 12:00 /dev/dri/card0
# crw-rw---- 1 root render 226, 128 Nov 12 12:00 /dev/dri/renderD128

# Typical groups:
# - video: Legacy GPU access
# - render: Modern render-only access (preferred)
```

**Docker Setup:**

Option 1: Use host user's groups
```bash
# Add your user to render/video group
sudo usermod -aG render,video $USER

# Run container with host user
docker run --user $(id -u):$(id -g) --group-add video ...
```

Option 2: Run as container user with group access
```yaml
services:
  xg2g-gpu:
    image: ghcr.io/manugh/xg2g:latest
    devices:
      - /dev/dri:/dev/dri
    group_add:
      - video   # GID from host system
      - render  # GID from host system
```

**Kubernetes Setup:**
```yaml
apiVersion: v1
kind: Pod
spec:
  containers:
    - name: xg2g
      securityContext:
        runAsUser: 65532
        runAsGroup: 65532
        supplementalGroups:
          - 44   # video group (check your system)
          - 109  # render group (check your system)
      volumeMounts:
        - name: dri
          mountPath: /dev/dri
  volumes:
    - name: dri
      hostPath:
        path: /dev/dri
        type: Directory
```

**Verify GPU Access:**
```bash
# Test VA-API availability in container
docker exec xg2g-container vainfo

# Expected output:
# Trying display: drm
# vainfo: VA-API version: 1.20 (libva 2.20.0)
# vainfo: Driver version: Mesa Gallium driver 23.3.6
```

### Required GPU Capabilities

**Intel Quick Sync Video (QSV):**
- GPU: Intel 6th Gen (Skylake) or newer
- Driver: i915 kernel module
- VA-API: mesa-va-drivers

**AMD VAAPI:**
- GPU: AMD Radeon with VCN support (Polaris+)
- Driver: amdgpu kernel module
- VA-API: mesa-va-drivers

**NVIDIA (experimental):**
- GPU: NVIDIA with NVENC support
- Driver: NVIDIA proprietary driver
- Toolkit: nvidia-docker2 runtime

---

## Security Best Practices

### 1. Container Security

✅ **DO:**
- Run as non-root user (default)
- Use read-only root filesystem
- Drop all unnecessary capabilities
- Set resource limits
- Use specific image tags (not `latest`)

❌ **DON'T:**
- Run with `--privileged` flag
- Mount host root directory
- Use `user: "0:0"` without justification
- Disable security features

### 2. Secrets Management

✅ **DO:**
- Use Kubernetes Secrets or Docker Secrets
- Use environment variables for credentials
- Rotate credentials regularly
- Use RBAC to restrict secret access

❌ **DON'T:**
- Commit secrets to Git
- Hardcode credentials in config files
- Share credentials across environments
- Log sensitive data

### 3. Network Security

✅ **DO:**
- Use NetworkPolicy in Kubernetes
- Restrict egress to known hosts
- Use TLS for external communication
- Enable firewall rules

❌ **DON'T:**
- Expose all ports publicly
- Disable network segmentation
- Allow unrestricted egress
- Use HTTP for sensitive data

### 4. Monitoring & Auditing

✅ **DO:**
- Enable audit logs in Kubernetes
- Monitor container syscalls
- Track file system changes
- Review security scan results

❌ **DON'T:**
- Ignore security alerts
- Disable logging for performance
- Skip vulnerability scanning
- Run without health checks

---

## Quick Reference

### Minimal Permissions (Standard Docker)

```yaml
version: '3.8'
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:latest
    user: "65532:65532"
    read_only: true
    cap_drop:
      - ALL
    cap_add:
      - NET_BIND_SERVICE
    ports:
      - "8080:8080"
    volumes:
      - xg2g-data:/data
      - /tmp
    environment:
      - XG2G_OWI_BASE=http://192.168.1.100
      - XG2G_BOUQUET=Favourites

volumes:
  xg2g-data:
    driver: local
```

### GPU Transcoding Permissions

```yaml
version: '3.8'
services:
  xg2g-gpu:
    image: ghcr.io/manugh/xg2g:latest
    devices:
      - /dev/dri:/dev/dri
    group_add:
      - video  # Host video group GID
    environment:
      - XG2G_ENABLE_GPU_TRANSCODING=true
      - XG2G_ENABLE_STREAM_PROXY=true
```

### Kubernetes Minimal RBAC

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: xg2g-deployer
  namespace: xg2g
rules:
  - apiGroups: ["", "apps", "networking.k8s.io"]
    resources: ["configmaps", "secrets", "services", "deployments", "ingresses"]
    verbs: ["create", "update", "get", "list"]
```

---

## Troubleshooting

### Permission Denied Errors

**Symptom:** Container fails to write to `/data`

**Solution:**
```bash
# Fix volume ownership
docker-compose down
sudo chown -R 65532:65532 /path/to/volume
docker-compose up
```

### GPU Access Denied

**Symptom:** `vainfo: error: can't connect to X server!` or `/dev/dri/renderD128: Permission denied`

**Solution:**
```bash
# Check device permissions
ls -l /dev/dri/renderD128

# Add container user to render group
docker run --group-add $(getent group render | cut -d: -f3) ...
```

### Kubernetes Pod Security Violation

**Symptom:** `Pod "xg2g-xxx" is forbidden: violates PodSecurity "restricted:latest"`

**Solution:**
```yaml
# Ensure all security contexts are set
securityContext:
  runAsNonRoot: true
  runAsUser: 65532
  allowPrivilegeEscalation: false
  capabilities:
    drop: [ALL]
  seccompProfile:
    type: RuntimeDefault
```

### Network Access Blocked

**Symptom:** Cannot reach OpenWebIF API

**Solution:**
```bash
# Test connectivity from container
docker exec xg2g curl -v http://192.168.1.100/api/status

# Check firewall rules on receiver
# Check NetworkPolicy in Kubernetes
kubectl describe networkpolicy -n xg2g
```

---

## Additional Resources

- [Docker Security Best Practices](https://docs.docker.com/engine/security/)
- [Kubernetes RBAC Documentation](https://kubernetes.io/docs/reference/access-authn-authz/rbac/)
- [Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/)
- [GitHub Actions Security Hardening](https://docs.github.com/en/actions/security-guides/security-hardening-for-github-actions)
- [OpenSSF Scorecard](https://scorecard.dev/)

---

## Contact

For security concerns, see [SECURITY.md](SECURITY.md).

For questions or issues, open a [GitHub Discussion](https://github.com/ManuGH/xg2g/discussions).
