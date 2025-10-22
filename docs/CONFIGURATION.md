# xg2g Configuration Guide

This guide covers all configuration options for xg2g and best practices for different deployment scenarios.

## Configuration Methods

xg2g supports three configuration methods with the following precedence (highest to lowest):

1. **Environment Variables** - Highest priority, overrides everything
2. **Configuration File** (YAML) - Middle priority
3. **Defaults** - Lowest priority (hard-coded)

### Why Use Config Files?

**Advantages:**
- ✅ Better organization and readability
- ✅ Version control friendly (separate secrets)
- ✅ Easier to manage complex configurations
- ✅ Environment variable interpolation (`${VAR}`)
- ✅ Validation before startup

**When to use ENV vars:**
- Secrets (passwords, API tokens)
- Environment-specific overrides (dev/staging/prod)
- Dynamic configuration in orchestrators (Kubernetes)

---

## Quick Start

### Minimal Configuration

The simplest config file requires only OpenWebIF URL and bouquet:

**config.yaml:**
```yaml
openWebIF:
  baseUrl: http://192.168.1.100

bouquets:
  - Favourites

epg:
  enabled: true
```

**Run:**
```bash
xg2g --config config.yaml
```

---

## Complete Configuration Reference

### Application Settings

```yaml
# Application version (usually auto-detected)
version: "1.5.0"

# Data directory for storing playlists and EPG files
# Default: /data
dataDir: /data

# Log level: debug, info, warn, error
# Default: info
logLevel: info
```

### OpenWebIF Configuration

**OpenWebIF** is the web interface of Enigma2 satellite/cable receivers.

```yaml
openWebIF:
  # Base URL of your Enigma2 receiver (required)
  baseUrl: http://192.168.1.100

  # HTTP Basic Auth credentials (optional)
  # Recommended: use environment variables for secrets
  username: root
  password: ${XG2G_OWI_PASSWORD}

  # Stream port (default: 8001)
  # This is where xg2g fetches video streams from
  streamPort: 8001

  # Request timeout (default: 10s)
  # Increase if receiver is slow to respond
  timeout: 10s

  # Number of retry attempts for failed requests (default: 3)
  retries: 3

  # Initial backoff duration between retries (default: 500ms)
  backoff: 500ms

  # Maximum backoff duration (default: 30s)
  # Prevents exponential backoff from growing too large
  maxBackoff: 30s
```

**Duration formats:**
- `500ms` - 500 milliseconds
- `5s` - 5 seconds
- `1m` - 1 minute
- `1h` - 1 hour

### Bouquets

Bouquets are channel groups on your Enigma2 receiver.

```yaml
# Single bouquet (simple)
bouquets:
  - Favourites

# Multiple bouquets (will be concatenated in order)
bouquets:
  - Favourites
  - Premium
  - Sports
  - Movies
```

**Equivalent ENV var:**
```bash
XG2G_BOUQUET="Favourites,Premium,Sports,Movies"
```

### EPG Configuration

Electronic Program Guide (EPG) provides schedule information for channels.

```yaml
epg:
  # Enable EPG collection (default: false)
  enabled: true

  # Number of days to fetch (1-14, default: 7)
  # More days = longer refresh time
  days: 7

  # Maximum concurrent EPG requests (1-10, default: 5)
  # Higher = faster but more load on receiver
  maxConcurrency: 5

  # Timeout per EPG request in milliseconds (default: 15000)
  # Increase if receiver is slow
  timeoutMs: 15000

  # Retry attempts for failed EPG requests (default: 2)
  retries: 2

  # Fuzzy matching max distance for channel names (default: 2)
  # Used to match EPG data to channels by name similarity
  fuzzyMax: 2

  # XMLTV output filename (default: xmltv.xml)
  # Relative to dataDir
  xmltvPath: xmltv.xml
```

**Performance tuning:**

| Scenario | Recommendation |
|----------|----------------|
| **Fast receiver, good network** | `maxConcurrency: 10`, `timeoutMs: 10000` |
| **Slow receiver, unstable network** | `maxConcurrency: 3`, `timeoutMs: 30000`, `retries: 5` |
| **Quick refresh (fewer programmes)** | `days: 3` |
| **Complete schedule** | `days: 14` |

### API Configuration

```yaml
api:
  # API authentication token (strongly recommended)
  # Use environment variable for security
  token: ${XG2G_API_TOKEN}

  # Listen address (default: :8080)
  # Format: :port or host:port
  listenAddr: :8080
```

**Security:**
- Always set an API token in production
- Use strong, randomly-generated tokens (e.g., `openssl rand -hex 32`)
- Store token in environment variable, not in config file

### Metrics Configuration

Prometheus-compatible metrics for monitoring.

```yaml
metrics:
  # Enable Prometheus metrics endpoint (default: false)
  enabled: true

  # Metrics listen address (default: :9090)
  # Should be different from API port
  listenAddr: :9090
```

**Metrics endpoint:** `http://localhost:9090/metrics`

### Picons (Channel Logos)

```yaml
picons:
  # Base URL for channel logos (optional)
  # Example: http://192.168.1.100:80/picon
  baseUrl: http://192.168.1.100:80/picon
```

If not set, playlists will not include logos.

---

## Environment Variable Interpolation

Config files support environment variable expansion:

```yaml
openWebIF:
  baseUrl: http://192.168.1.100
  username: root
  password: ${XG2G_OWI_PASSWORD}  # Expanded at runtime

api:
  token: ${XG2G_API_TOKEN}
```

**Syntax:**
- `${VAR}` - Expands to value of `VAR`
- `$VAR` - Also supported
- Undefined variables expand to empty string

---

## Environment Variable Reference

All config file settings can be overridden via environment variables.

### Core Settings

| ENV Variable | Config Path | Type | Default |
|--------------|-------------|------|---------|
| `XG2G_DATA` | `dataDir` | string | `/data` |
| `XG2G_OWI_BASE` | `openWebIF.baseUrl` | string | `http://10.10.55.57` |
| `XG2G_OWI_USER` | `openWebIF.username` | string | - |
| `XG2G_OWI_PASS` | `openWebIF.password` | string | - |
| `XG2G_STREAM_PORT` | `openWebIF.streamPort` | int | `8001` |
| `XG2G_OWI_TIMEOUT_MS` | `openWebIF.timeout` | duration (ms) | `10000` |
| `XG2G_OWI_RETRIES` | `openWebIF.retries` | int | `3` |
| `XG2G_OWI_BACKOFF_MS` | `openWebIF.backoff` | duration (ms) | `500` |
| `XG2G_BOUQUET` | `bouquets` | string (comma-sep) | `Premium` |

### EPG Settings

| ENV Variable | Config Path | Type | Default |
|--------------|-------------|------|---------|
| `XG2G_EPG_ENABLED` | `epg.enabled` | bool | `false` |
| `XG2G_EPG_DAYS` | `epg.days` | int | `7` |
| `XG2G_EPG_MAX_CONCURRENCY` | `epg.maxConcurrency` | int | `5` |
| `XG2G_EPG_TIMEOUT_MS` | `epg.timeoutMs` | int | `15000` |
| `XG2G_EPG_RETRIES` | `epg.retries` | int | `2` |
| `XG2G_FUZZY_MAX` | `epg.fuzzyMax` | int | `2` |
| `XG2G_XMLTV` | `epg.xmltvPath` | string | - |

### API Settings

| ENV Variable | Config Path | Type | Default |
|--------------|-------------|------|---------|
| `XG2G_API_TOKEN` | `api.token` | string | - |

### Picons

| ENV Variable | Config Path | Type | Default |
|--------------|-------------|------|---------|
| `XG2G_PICON_BASE` | `picons.baseUrl` | string | - |

---

## Example Configurations

### Development

**config.dev.yaml:**
```yaml
dataDir: ./data
logLevel: debug

openWebIF:
  baseUrl: http://192.168.1.100
  streamPort: 8001

bouquets:
  - Favourites

epg:
  enabled: true
  days: 3  # Faster refresh
  maxConcurrency: 3

api:
  listenAddr: :8080
```

### Production

**config.prod.yaml:**
```yaml
dataDir: /data
logLevel: info

openWebIF:
  baseUrl: ${XG2G_OWI_BASE}
  username: ${XG2G_OWI_USER}
  password: ${XG2G_OWI_PASSWORD}
  streamPort: 8001
  timeout: 15s
  retries: 5
  backoff: 1s

bouquets:
  - Premium
  - Favourites
  - Movies
  - Sports

epg:
  enabled: true
  days: 7
  maxConcurrency: 10
  timeoutMs: 20000
  retries: 3

api:
  token: ${XG2G_API_TOKEN}
  listenAddr: :8080

metrics:
  enabled: true
  listenAddr: :9090

picons:
  baseUrl: ${XG2G_PICON_BASE}
```

**.env (secrets):**
```bash
XG2G_OWI_BASE=http://192.168.1.100
XG2G_OWI_USER=root
XG2G_OWI_PASSWORD=secret123
XG2G_API_TOKEN=your-long-random-token
XG2G_PICON_BASE=http://192.168.1.100:80/picon
```

### Docker

**docker-compose.yml:**
```yaml
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:latest
    command: ["--config", "/config/config.yaml"]
    environment:
      - XG2G_OWI_PASSWORD=${XG2G_OWI_PASSWORD}
      - XG2G_API_TOKEN=${XG2G_API_TOKEN}
    volumes:
      - ./data:/data
      - ./config.yaml:/config/config.yaml:ro
    ports:
      - "8080:8080"
      - "9090:9090"  # Metrics
    restart: unless-stopped
```

### Kubernetes

**configmap.yaml:**
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: xg2g-config
data:
  config.yaml: |
    dataDir: /data
    openWebIF:
      baseUrl: http://enigma2.local
      streamPort: 8001
    bouquets:
      - Favourites
    epg:
      enabled: true
      days: 7
```

**secret.yaml:**
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: xg2g-secrets
type: Opaque
stringData:
  owi-password: "secret123"
  api-token: "your-token"
```

**deployment.yaml:**
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: xg2g
spec:
  template:
    spec:
      containers:
      - name: xg2g
        image: ghcr.io/manugh/xg2g:latest
        args: ["--config", "/config/config.yaml"]
        env:
        - name: XG2G_OWI_PASSWORD
          valueFrom:
            secretKeyRef:
              name: xg2g-secrets
              key: owi-password
        - name: XG2G_API_TOKEN
          valueFrom:
            secretKeyRef:
              name: xg2g-secrets
              key: api-token
        volumeMounts:
        - name: config
          mountPath: /config
        - name: data
          mountPath: /data
      volumes:
      - name: config
        configMap:
          name: xg2g-config
      - name: data
        persistentVolumeClaim:
          claimName: xg2g-data
```

---

## Best Practices

### 1. Separate Configuration from Secrets

**Good:**
```yaml
# config.yaml (version controlled)
openWebIF:
  baseUrl: http://192.168.1.100
  password: ${XG2G_OWI_PASSWORD}

# .env (not in git)
XG2G_OWI_PASSWORD=secret123
```

**Bad:**
```yaml
# config.yaml (DO NOT commit passwords!)
openWebIF:
  password: secret123  # ❌ Security risk!
```

### 2. Use Descriptive Config Files

```bash
config/
├── config.dev.yaml       # Development
├── config.staging.yaml   # Staging
├── config.prod.yaml      # Production
└── config.test.yaml      # Testing
```

```bash
# Select config at runtime
xg2g --config config/config.prod.yaml
```

### 3. Document Your Config

Add comments to explain non-obvious settings:

```yaml
epg:
  # Increased timeout because receiver is on slow network
  timeoutMs: 30000

  # Reduced concurrency to prevent receiver overload
  maxConcurrency: 3
```

### 4. Validate Before Deployment

```bash
# Test config file loading
xg2g --config config.yaml --version
```

### 5. Monitor Configuration Changes

Use version control to track config changes:

```bash
git log -- config.yaml
```

---

## Troubleshooting

### Config file not loaded

**Symptom:** xg2g uses defaults instead of config file

**Check:**
1. Is `--config` flag set?
   ```bash
   xg2g --config /path/to/config.yaml
   ```

2. Is path correct?
   ```bash
   ls -la /path/to/config.yaml
   ```

3. Check logs for "config.loaded" event:
   ```bash
   docker logs xg2g | grep config.loaded
   ```

### Environment variables not expanding

**Symptom:** Password shows as `${VAR}` instead of actual value

**Solution:**
Ensure environment variable is set:

```bash
# Check if variable is set
echo $XG2G_OWI_PASSWORD

# Set variable
export XG2G_OWI_PASSWORD=secret123
```

### Invalid YAML syntax

**Symptom:** Error: "parse YAML: ..."

**Solution:**
Validate YAML syntax:

```bash
# Online: https://www.yamllint.com/
# CLI:
yamllint config.yaml
```

**Common mistakes:**
```yaml
# ❌ Wrong indentation
openWebIF:
username: root  # Should be indented

# ✅ Correct
openWebIF:
  username: root

# ❌ Missing colon
openWebIF
  baseUrl http://...

# ✅ Correct
openWebIF:
  baseUrl: http://...
```

### Config not taking effect

**Remember:** ENV variables override config file!

```bash
# This ENV var overrides config.yaml
export XG2G_BOUQUET="Override"
```

**Solution:**
```bash
# Unset ENV var to use config file value
unset XG2G_BOUQUET
```

---

## Migration from ENV-only Setup

### Step 1: Create config file

Extract current ENV vars into `config.yaml`:

**Before (docker-compose.yml):**
```yaml
environment:
  - XG2G_OWI_BASE=http://192.168.1.100
  - XG2G_BOUQUET=Favourites
  - XG2G_EPG_ENABLED=true
  - XG2G_EPG_DAYS=7
```

**After (config.yaml):**
```yaml
openWebIF:
  baseUrl: http://192.168.1.100
bouquets:
  - Favourites
epg:
  enabled: true
  days: 7
```

### Step 2: Keep secrets in ENV

**docker-compose.yml:**
```yaml
command: ["--config", "/config/config.yaml"]
environment:
  - XG2G_OWI_PASSWORD=${XG2G_OWI_PASSWORD}
  - XG2G_API_TOKEN=${XG2G_API_TOKEN}
volumes:
  - ./config.yaml:/config/config.yaml:ro
```

### Step 3: Test

```bash
# Stop old container
docker-compose down

# Start with config file
docker-compose up -d

# Check logs
docker-compose logs -f
```

---

## See Also

- [README.md](../README.md) - Quick start guide
- [API_V1_CONTRACT.md](API_V1_CONTRACT.md) - API documentation
- [examples/](../examples/) - Example config files
