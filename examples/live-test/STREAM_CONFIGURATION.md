# Stream Configuration - xg2g

## Übersicht

xg2g unterstützt zwei Modi für Stream-URLs:

### Option A: Direkter TS Stream (Port 8001) ✅ Empfohlen

**Wann nutzen:**
- Enigma2 Receiver unterstützt HEAD-Requests auf Port 8001
- Niedrigste Latenz gewünscht
- Kein zusätzlicher Proxy nötig

**Konfiguration:**
```yaml
# docker-compose.yml
environment:
  - XG2G_STREAM_PORT=8001
  # Keine weiteren Proxy-Variablen setzen!
```

**Ergebnis:**
```
Stream-URL: http://10.10.55.57:8001/1:0:19:81:6:85:C00000:0:0:0:
Flow: Jellyfin → Threadfin → Enigma2 (Port 8001)
```

---

### Option B: Mit integriertem Stream Proxy 🚀 NEU!

**Wann nutzen:**
- Enigma2 Stream Relay (Port 17999) unterstützt **keine** HEAD-Requests
- Threadfin/Jellyfin zeigt "EOF" Fehler
- Receiver soll im Standby bleiben können

**Vorteile:**
- ✅ **Kein separater nginx-Container nötig!**
- ✅ Integrierter Go Reverse-Proxy in xg2g
- ✅ Automatisches HEAD-Request-Handling
- ✅ Nur ~50 Zeilen Go-Code, keine Dependencies

**Konfiguration:**

```yaml
# docker-compose.yml
services:
  xg2g:
    ports:
      - "8080:8080"    # API
      - "18000:18000"  # Stream Proxy
    environment:
      # Enigma2 Receiver
      - XG2G_OWI_BASE=http://10.10.55.57

      # Stream Proxy aktivieren
      - XG2G_ENABLE_STREAM_PROXY=true
      - XG2G_PROXY_PORT=18000
      - XG2G_PROXY_TARGET=http://10.10.55.57:17999

      # Stream URLs auf Proxy zeigen
      - XG2G_STREAM_BASE=http://10.10.55.50:18000  # Host-IP!
```

**Ergebnis:**
```
Stream-URL: http://10.10.55.50:18000/1:0:19:81:6:85:C00000:0:0:0:
Flow: Jellyfin → Threadfin → xg2g Proxy (HEAD support) → Enigma2 (Port 17999)
```

---

## Environment Variables

| Variable | Beschreibung | Default | Beispiel |
|----------|--------------|---------|----------|
| `XG2G_STREAM_PORT` | Enigma2 Stream Port (Option A) | `8001` | `8001` oder `17999` |
| `XG2G_STREAM_BASE` | Überschreibt Stream Host/Port | - | `http://10.10.55.50:18000` |
| `XG2G_ENABLE_STREAM_PROXY` | Aktiviert integrierten Proxy | `false` | `true` |
| `XG2G_PROXY_PORT` | Proxy Listen Port | `18000` | `18000`, `19000`, etc. |
| `XG2G_PROXY_TARGET` | Enigma2 Stream Relay URL | - | `http://10.10.55.57:17999` |

---

## Wie funktioniert der integrierte Proxy?

**Code-Logik in xg2g:**

1. **HEAD-Requests werden direkt beantwortet:**
```go
if r.Method == http.MethodHead {
    w.Header().Set("Content-Type", "video/mp2t")
    w.WriteHeader(http.StatusOK)
    return  // Kein Proxy zu Enigma2!
}
```

2. **GET-Requests werden an Enigma2 weitergeleitet:**
```go
proxy := httputil.NewSingleHostReverseProxy(targetURL)
proxy.ServeHTTP(w, r)
```

**Vorteile gegenüber nginx:**
- Keine extra nginx.conf Datei
- Kein separater Container
- Pure Go, keine Dependencies
- Kleiner Memory-Footprint (~5MB)
- Automatisches Logging mit xg2g

---

## Troubleshooting

### Problem: Threadfin zeigt "EOF" Fehler

**Ursache:** Enigma2 Stream Relay unterstützt keine HEAD-Requests

**Test:**
```bash
curl -I http://10.10.55.57:17999/test
# Zeigt: Empty reply from server (EOF)
```

**Lösung:** Option B verwenden (integrierter Proxy)

---

### Problem: Stream URLs zeigen auf Enigma2 statt Proxy

**Ursache:** `XG2G_STREAM_BASE` nicht gesetzt

**Prüfen:**
```bash
docker exec xg2g env | grep STREAM
# Sollte zeigen: XG2G_STREAM_BASE=http://...
```

**Fix:**
```yaml
- XG2G_STREAM_BASE=http://<HOST-IP>:18000
```

---

### Problem: Proxy startet nicht

**Logs prüfen:**
```bash
docker logs xg2g | grep proxy
# Erwartung: "starting stream proxy server"
```

**Häufige Fehler:**
1. `XG2G_ENABLE_STREAM_PROXY` nicht auf `true`
2. `XG2G_PROXY_TARGET` nicht gesetzt
3. Port bereits belegt

---

## Migration von nginx zu integriertem Proxy

**Alt (nginx):**
```yaml
services:
  nginx-stream-proxy:  # ← Kann entfernt werden!
    image: nginx:alpine
    ports:
      - "18000:17999"
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf:ro

  xg2g:
    environment:
      - XG2G_STREAM_BASE=http://10.10.55.50:18000
    depends_on:
      - nginx-stream-proxy
```

**Neu (integrierter Proxy):**
```yaml
services:
  xg2g:
    ports:
      - "8080:8080"
      - "18000:18000"  # ← Stream Proxy Port
    environment:
      - XG2G_ENABLE_STREAM_PROXY=true
      - XG2G_PROXY_PORT=18000
      - XG2G_PROXY_TARGET=http://10.10.55.57:17999
      - XG2G_STREAM_BASE=http://10.10.55.50:18000
```

**Vorteile:**
- ✅ Ein Container weniger
- ✅ Keine nginx.conf Datei nötig
- ✅ Einfachere Wartung
- ✅ Besseres Logging (alles in xg2g)

---

## Empfehlung

| Szenario | Empfohlene Option |
|----------|-------------------|
| **Production, Port 8001 funktioniert** | Option A (Direkter Stream) |
| **Port 17999 benötigt (Standby)** | Option B (Integrierter Proxy) |
| **Development/Testing** | Option B (Maximale Kompatibilität) |
| **Docker Swarm/Kubernetes** | Option B (Weniger Services) |

---

## Beispiel-Konfigurationen

### Minimal (Option A - Direkt)
```yaml
xg2g:
  environment:
    - XG2G_OWI_BASE=http://10.10.55.57
    - XG2G_STREAM_PORT=8001
```

### Vollständig (Option B - Proxy)
```yaml
xg2g:
  ports:
    - "8080:8080"
    - "18000:18000"
  environment:
    - XG2G_OWI_BASE=http://10.10.55.57
    - XG2G_ENABLE_STREAM_PROXY=true
    - XG2G_PROXY_PORT=18000
    - XG2G_PROXY_TARGET=http://10.10.55.57:17999
    - XG2G_STREAM_BASE=http://10.10.55.50:18000
```

---

## FAQ

**Q: Kann ich beide Modi gleichzeitig nutzen?**
A: Nein, entweder Option A ODER Option B.

**Q: Benötigt der Proxy viel Ressourcen?**
A: Nein, nur ~5MB RAM, keine CPU-Last (nur HEAD-Requests).

**Q: Kann ich einen anderen Port als 18000 nutzen?**
A: Ja, `XG2G_PROXY_PORT` ist frei wählbar.

**Q: Funktioniert das mit Jellyfin/Plex/Emby?**
A: Ja, alle IPTV-Player die HEAD-Requests machen.

**Q: Was ist mit SSL/TLS?**
A: Aktuell nicht unterstützt, aber planbar für zukünftige Versionen.
