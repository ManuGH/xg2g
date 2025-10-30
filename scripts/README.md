# xg2g Deployment Scripts

This directory contains deployment and management scripts for all xg2g modes.

## Scripts Overview

### MODE 3: GPU Transcoding Scripts

#### test-gpu-mode.sh

Automated testing script for GPU Transcoding implementation (MODE 3).

**Features:**
- Docker image build verification
- FFI symbol checking
- Container startup tests (with/without GPU)
- Health endpoint validation
- Metrics endpoint testing
- Production GPU testing

**Usage:**
```bash
# Full test (build + all phases)
./scripts/test-gpu-mode.sh

# Skip build (use existing image)
./scripts/test-gpu-mode.sh --skip-build

# Production test (requires GPU)
./scripts/test-gpu-mode.sh --production
```

#### deploy-gpu-mode.sh

Production deployment script for GPU transcoding mode.

**Features:**
- SSH connection verification
- GPU hardware detection (VAAPI)
- Automatic port configuration
- Health checks after deployment
- Rollback to MODE 2 capability

**Usage:**
```bash
# Deploy MODE 3 to production
./scripts/deploy-gpu-mode.sh

# Rollback to MODE 2
./scripts/deploy-gpu-mode.sh --rollback
```

### MODE 2: Audio Proxy Scripts

#### deploy-stream-proxy.sh

Automated deployment script for xg2g stream proxy with configurable backend routing (MODE 2).

**Features:**
- Configurable backend port (default: 8001, alternative: 17999)
- Automatic process cleanup
- Health checks and validation
- Environment-based configuration

## Quick Start

### Standard Deployment (Direct Tuner - Port 8001)

```bash
# Export receiver IP
export RECEIVER_IP=192.168.1.100

# Deploy with default backend port (8001)
./scripts/deploy-stream-proxy.sh
```

### Alternative Backend Deployment (Port 17999)

```bash
# Export receiver IP
export RECEIVER_IP=192.168.1.100

# Deploy with alternative backend port (17999)
./scripts/deploy-stream-proxy.sh 17999
```

## Configuration

### Required Environment Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `RECEIVER_IP` | VU+ Enigma2 receiver IP address | `192.168.1.100` |

### Optional Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `PROXY_LISTEN_PORT` | Public proxy listen port | `18000` |
| `API_LISTEN_PORT` | API server listen port | `18080` |
| `XG2G_INSTALL_DIR` | xg2g installation directory | `/root/xg2g` |
| `XG2G_BOUQUET` | Bouquet name | `Favourites (TV)` |
| `RUST_LIB_PATH` | Rust library path | `$XG2G_INSTALL_DIR/transcoder/target/release` |

## Detailed Examples

### Example 1: Standard Setup

```bash
#!/bin/bash
export RECEIVER_IP=192.168.1.100
./scripts/deploy-stream-proxy.sh
```

**Result:**
- Proxy Listen: `:18000`
- Backend Target: `http://192.168.1.100:8001`
- API Server: `:18080`

### Example 2: Alternative Backend

```bash
#!/bin/bash
export RECEIVER_IP=192.168.1.100
./scripts/deploy-stream-proxy.sh 17999
```

**Result:**
- Proxy Listen: `:18000`
- Backend Target: `http://192.168.1.100:17999`
- API Server: `:18080`

### Example 3: Custom Ports

```bash
#!/bin/bash
export RECEIVER_IP=192.168.1.100
export PROXY_LISTEN_PORT=19000
export API_LISTEN_PORT=19080
./scripts/deploy-stream-proxy.sh
```

**Result:**
- Proxy Listen: `:19000`
- Backend Target: `http://192.168.1.100:8001`
- API Server: `:19080`

### Example 4: Custom Installation Directory

```bash
#!/bin/bash
export RECEIVER_IP=192.168.1.100
export XG2G_INSTALL_DIR=/opt/xg2g
export RUST_LIB_PATH=/opt/xg2g/transcoder/target/release
./scripts/deploy-stream-proxy.sh 17999
```

## Usage Patterns

### Development Testing

```bash
# Quick deploy for testing
export RECEIVER_IP=192.168.1.100
./scripts/deploy-stream-proxy.sh

# View logs
tail -f /tmp/xg2g-stream-proxy.log

# Test streaming
curl -I http://localhost:18000/1:0:19:132F:3EF:1:C00000:0:0:0:
```

### Production Deployment

```bash
# Create deployment script
cat > /opt/xg2g/start.sh << 'EOF'
#!/bin/bash
export RECEIVER_IP=192.168.1.100
export XG2G_INSTALL_DIR=/opt/xg2g
export RUST_LIB_PATH=/opt/xg2g/transcoder/target/release
exec /opt/xg2g/scripts/deploy-stream-proxy.sh 17999
EOF

chmod +x /opt/xg2g/start.sh

# Create systemd service
cat > /etc/systemd/system/xg2g.service << 'EOF'
[Unit]
Description=xg2g Stream Proxy
After=network.target

[Service]
Type=forking
ExecStart=/opt/xg2g/start.sh
Restart=on-failure
RestartSec=10s

[Install]
WantedBy=multi-user.target
EOF

# Enable and start
systemctl daemon-reload
systemctl enable xg2g
systemctl start xg2g
```

### Multi-Instance Deployment

```bash
# Instance 1: Direct tuner (port 8001)
export RECEIVER_IP=192.168.1.100
export PROXY_LISTEN_PORT=18000
export API_LISTEN_PORT=18080
./scripts/deploy-stream-proxy.sh &

# Instance 2: Alternative backend (port 17999)
export RECEIVER_IP=192.168.1.100
export PROXY_LISTEN_PORT=18001
export API_LISTEN_PORT=18081
./scripts/deploy-stream-proxy.sh 17999 &

wait
```

## Management

### Check Status

```bash
# Process status
ps aux | grep xg2g-daemon

# View logs
tail -f /tmp/xg2g-stream-proxy.log

# Health check
curl http://localhost:18080/api/status
```

### Stop Service

```bash
pkill -9 xg2g
```

### Restart Service

```bash
pkill -9 xg2g
sleep 2
export RECEIVER_IP=192.168.1.100
./scripts/deploy-stream-proxy.sh
```

## Troubleshooting

### Deployment Fails

**Check logs:**
```bash
tail -50 /tmp/xg2g-stream-proxy.log
```

**Common issues:**
- Missing `RECEIVER_IP` environment variable
- xg2g binary not found in installation directory
- Rust library path incorrect

### Streams Not Working

**Test backend connectivity:**
```bash
curl -I http://RECEIVER_IP:BACKEND_PORT/1:0:19:132F:3EF:1:C00000:0:0:0:
```

**Check proxy logs:**
```bash
grep "error" /tmp/xg2g-stream-proxy.log
```

### Audio-Video Desync

**Verify Rust remuxer is enabled:**
```bash
grep "rust remuxer" /tmp/xg2g-stream-proxy.log
```

**Check library path:**
```bash
ls -la $RUST_LIB_PATH/libac_remuxer.so
```

## Backend Port Selection Guide

| Use Case | Port | Rationale |
|----------|------|-----------|
| Standard streaming | 8001 | Direct tuner access, minimal latency (5-10ms) |
| Alternative backend | 17999 | Custom routing for specialized setups (10-20ms) |
| Development | 8001 | Standard default configuration |
| Testing | Both | Test different routing paths |

## Performance Expectations

### Resource Usage

```
CPU Usage:    0.0% (idle), <5% (streaming)
Memory:       ~39 MB RSS
Throughput:   0.96 MB/s (input-limited by tuner)
```

### Latency

```
Backend Port 8001:  5-10 ms overhead
Backend Port 17999: 10-20 ms overhead

Note: Both latencies are imperceptible for streaming
      (iOS Safari buffer: 2-4 seconds)
```

## See Also

- [STREAM_PROXY_ROUTING.md](../docs/STREAM_PROXY_ROUTING.md) - Architecture documentation
- [PHASE_5_IMPLEMENTATION_PLAN.md](../docs/PHASE_5_IMPLEMENTATION_PLAN.md) - AC3→AAC transcoding
- [RUST_REMUXER_INTEGRATION.md](../docs/RUST_REMUXER_INTEGRATION.md) - Rust remuxer details
- [SECURITY_TESTS.md](./SECURITY_TESTS.md) - Security testing documentation

## License

MIT License - See LICENSE file for details
