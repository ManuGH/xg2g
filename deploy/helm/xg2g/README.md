# xg2g Helm Chart

Enigma2 to IPTV Gateway with M3U and XMLTV support for Kubernetes deployments.

## Prerequisites

- Kubernetes 1.19+
- Helm 3.0+
- PersistentVolume provisioner support (if persistence is enabled)

## Installing the Chart

To install the chart with the release name `my-xg2g`:

```bash
helm install my-xg2g ./xg2g \
  --set config.owiBase=http://your-receiver-ip \
  --set config.bouquet="Favourites"
```

## Uninstalling the Chart

To uninstall/delete the `my-xg2g` deployment:

```bash
helm delete my-xg2g
```

## Configuration

The following table lists the configurable parameters of the xg2g chart and their default values.

### Image Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `image.repository` | xg2g image repository | `ghcr.io/manugh/xg2g` |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `image.tag` | Image tag (overrides appVersion) | `""` |

### Service Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `service.type` | Kubernetes service type | `ClusterIP` |
| `service.port` | Service port | `8080` |

### Persistence

| Parameter | Description | Default |
|-----------|-------------|---------|
| `persistence.enabled` | Enable persistence | `true` |
| `persistence.size` | PersistentVolumeClaim size | `1Gi` |
| `persistence.accessMode` | PVC access mode | `ReadWriteOnce` |
| `persistence.storageClass` | Storage class | `""` |

### xg2g Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `config.owiBase` | Enigma2 receiver URL | `http://your-receiver-ip` |
| `config.owiUser` | OpenWebIF username | `""` |
| `config.owiPass` | OpenWebIF password | `""` |
| `config.bouquet` | Bouquet name | `Favourites` |
| `config.epg.enabled` | Enable EPG generation | `true` |
| `config.epg.days` | EPG days to fetch | `7` |
| `config.hdhr.enabled` | Enable HDHomeRun emulation | `true` |
| `config.hdhr.friendlyName` | HDHomeRun device name | `xg2g` |
| `config.streamProxy.enabled` | Enable stream proxy | `false` |
| `config.transcoding.enabled` | Enable audio transcoding | `false` |
| `config.logLevel` | Log level | `info` |

### Ingress

| Parameter | Description | Default |
|-----------|-------------|---------|
| `ingress.enabled` | Enable ingress | `false` |
| `ingress.className` | Ingress class name | `""` |
| `ingress.hosts` | Ingress hosts | `[{host: "xg2g.local", paths: [{path: "/", pathType: "Prefix"}]}]` |

### Resources

| Parameter | Description | Default |
|-----------|-------------|---------|
| `resources.limits.cpu` | CPU limit | `500m` |
| `resources.limits.memory` | Memory limit | `256Mi` |
| `resources.requests.cpu` | CPU request | `100m` |
| `resources.requests.memory` | Memory request | `128Mi` |

## Example: Production Deployment

```bash
helm install xg2g ./xg2g \
  --set config.owiBase=http://192.168.1.100 \
  --set config.owiPass="your-password" \
  --set config.bouquet="Premium Channels" \
  --set config.epg.enabled=true \
  --set config.epg.days=7 \
  --set persistence.size=5Gi \
  --set resources.limits.memory=512Mi \
  --set ingress.enabled=true \
  --set ingress.hosts[0].host=xg2g.example.com
```

## Example: With Stream Proxy and Transcoding

```bash
helm install xg2g ./xg2g \
  --set config.owiBase=http://receiver.local \
  --set config.streamProxy.enabled=true \
  --set config.streamProxy.port=18000 \
  --set config.transcoding.enabled=true \
  --set config.transcoding.audioCodec=aac
```

## Monitoring

To enable Prometheus metrics:

```bash
helm install xg2g ./xg2g \
  --set metrics.enabled=true \
  --set metrics.serviceMonitor.enabled=true
```

## Upgrading

```bash
helm upgrade xg2g ./xg2g \
  --reuse-values \
  --set image.tag=1.7.1
```

## Values Files

You can create a custom `values.yaml` file:

```yaml
config:
  owiBase: "http://my-receiver.local"
  owiPass: "my-password"
  bouquet: "My Favorites"
  epg:
    enabled: true
    days: 14
  hdhr:
    enabled: true
    friendlyName: "Living Room TV"

persistence:
  size: 10Gi
  storageClass: "fast-ssd"

ingress:
  enabled: true
  className: "nginx"
  hosts:
    - host: tv.home.lan
      paths:
        - path: /
          pathType: Prefix

resources:
  limits:
    cpu: 1000m
    memory: 512Mi
  requests:
    cpu: 200m
    memory: 256Mi
```

Then install:

```bash
helm install xg2g ./xg2g -f my-values.yaml
```

## Troubleshooting

### Check pod status

```bash
kubectl get pods -l app.kubernetes.io/name=xg2g
```

### View logs

```bash
kubectl logs -l app.kubernetes.io/name=xg2g -f
```

### Access the API

```bash
kubectl port-forward svc/xg2g 8080:8080
curl http://localhost:8080/api/status
```

## Support

For issues and feature requests, please visit https://github.com/ManuGH/xg2g/issues
