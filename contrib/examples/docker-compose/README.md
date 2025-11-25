# Docker Compose Example

This is a ready-to-use Docker Compose setup for xg2g.

## Quick Start

```bash
# 1. Copy the example file
cp docker-compose.yml /path/to/your/setup/

# 2. Generate an API token (required for /api/refresh endpoint)
openssl rand -hex 16

# 3. Edit and adjust for your receiver
nano docker-compose.yml
# Change:
#   - XG2G_OWI_BASE=http://YOUR_RECEIVER_IP
#   - XG2G_BOUQUET=YOUR_BOUQUET_NAME
#   - XG2G_API_TOKEN=your-generated-token  # Add this!
# If your receiver requires authentication, uncomment:
#   - XG2G_OWI_USER=root
#   - XG2G_OWI_PASS=your-password

# 4. Start the service
docker compose up -d

# 5. Check logs
docker compose logs -f xg2g

# 6. Access your files
# M3U:  http://localhost:8080/files/playlist.m3u
# XMLTV: http://localhost:8080/files/xmltv.xml
```

## Using .env file (Optional)

If you prefer environment files:

```bash
# Copy the example
cp .env.example .env

# Edit values
nano .env

# Update docker-compose.yml to use env_file:
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:latest
    env_file: .env
    # ... rest of config
```

## Configuration

**Required:**
- `XG2G_OWI_BASE` - Your receiver's IP address
- `XG2G_BOUQUET` - Bouquet name from your receiver

**Optional but recommended:**
- `XG2G_XMLTV=xmltv.xml` - Generate XMLTV EPG file

See the [main README](../../README.md#configuration-env) for all available options.

## Troubleshooting

**Container keeps restarting:**
```bash
docker compose logs xg2g
```

**Check if receiver is reachable:**
```bash
curl http://YOUR_RECEIVER_IP/api/statusinfo
```

**Permission issues with ./data:**
The container runs as root by default, so this should work out of the box.

## Next Steps

- Add to xTeVe/Threadfin with the M3U and XMLTV URLs
- Enable Prometheus metrics for monitoring
- See [Production Deployment](../../docs/PRODUCTION.md) for advanced setups
