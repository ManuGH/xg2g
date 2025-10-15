# 📺 Threadfin Integration - xg2g v0.3.0 EPG Ready

## 🎯 Quick Setup für Threadfin

**xg2g Service URLs:**
```text
M3U Playlist:  http://localhost:8080/files/playlist.m3u
XMLTV EPG:     http://localhost:8080/files/xmltv.xml
```

## 🔧 Threadfin Konfiguration

### 1. M3U Playlist hinzufügen
```text
Name: xg2g Premium Bouquet
URL:  http://localhost:8080/files/playlist.m3u
Type: M3U
```

### 2. XMLTV EPG hinzufügen
```text
Name: xg2g EPG Data
URL:  http://localhost:8080/files/xmltv.xml
Type: XMLTV
```

### 3. Channel Mapping
- **Automatisch**: Channel IDs sind bereits abgestimmt
- **tvg-id**: Verwendet stable IDs aus Service References
- **Kanalnamen**: Identisch zwischen M3U und XMLTV

## ✅ Validierung

### Aktuelle Statistiken (v0.3.0)
- **Kanäle**: 133 (Premium Bouquet)
- **Programme**: 129 (EPG-Daten verfügbar)
- **Format**: IPTV-Standard konform
- **Update-Intervall**: Auf Abruf via `/api/refresh`

### Test-URLs
```bash
# M3U Test
curl -s http://localhost:8080/files/playlist.m3u | head -n 10

# XMLTV Test  
curl -s http://localhost:8080/files/xmltv.xml | grep -A 3 '<programme'

# Kanal-Count
curl -s http://localhost:8080/files/playlist.m3u | grep -c '^#EXTINF'  # 133
curl -s http://localhost:8080/files/xmltv.xml | grep -c '<programme'    # 129
```

## 🔄 Auto-Refresh Integration

**Threadfin Refresh-Trigger:**
```bash
# Trigger xg2g refresh vor Threadfin update
curl -X POST http://localhost:8080/api/refresh \
  -H "X-API-Token: YOUR_API_TOKEN_HERE"

# Dann Threadfin refresh
# (Threadfin holt automatisch neue M3U/XMLTV)
```

## 📊 Monitoring

**Health-Check URLs:**
```text
Status:     http://localhost:8080/api/status
Health:     http://localhost:8080/healthz
Readiness:  http://localhost:8080/readyz
Metrics:    http://localhost:9090/metrics
```

**Key Metriken:**
- `xg2g_epg_programmes_collected`: Anzahl EPG-Programme
- `xg2g_xmltv_channels_written`: Kanäle in XMLTV
- `xg2g_refresh_duration_seconds`: Refresh-Performance

## 🎯 Production Setup

**Für Remote-Access:**
```text
M3U:   http://<your-server-ip>:8080/files/playlist.m3u
XMLTV: http://<your-server-ip>:8080/files/xmltv.xml
```

**Docker Compose (empfohlen):**
```yaml
# docker-compose.yml
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:v0.3.0
    ports:
      - "8080:8080"
      - "9090:9090"  # Metrics
    environment:
      - XG2G_EPG_ENABLED=true
      # ... weitere Config via .env
```

## 💡 Tipps

### EPG validieren

```bash
curl -s http://localhost:8080/files/xmltv.xml | grep -c '<programme'
# Sollte > 0 sein
```

### Stream-Port Troubleshooting

Standard ist Port **8001**. Falls Streams nicht funktionieren, versuche Port **17999** (Stream Relay):

```bash
XG2G_STREAM_PORT=17999
```

### Empfohlene Konfiguration

```bash
XG2G_STREAM_PORT=8001            # Standard Stream Port
XG2G_USE_WEBIF_STREAMS=false     # Direkte TS Streams
XG2G_EPG_ENABLED=true            # EPG aktiviert
XG2G_EPG_DAYS=7                  # 7 Tage EPG-Daten
```

---
**Status: ✅ READY FOR THREADFIN INTEGRATION**
**Performance: 133 Kanäle, 129 Programme, ~100ms Refresh**
