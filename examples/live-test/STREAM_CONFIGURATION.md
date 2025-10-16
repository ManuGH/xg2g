# Stream Configuration - xg2g + Threadfin + Jellyfin

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
  # XG2G_STREAM_BASE nicht setzen!
```

**Ergebnis:**
```
Stream-URL: http://10.10.55.57:8001/1:0:19:81:6:85:C00000:0:0:0:
Flow: Jellyfin → Threadfin → Enigma2 (Port 8001)
```

---

### Option B: Via nginx Proxy (Port 17999) 🔧 Für Kompatibilität

**Wann nutzen:**
- Enigma2 Stream Relay (Port 17999) unterstützt **keine** HEAD-Requests
- Threadfin Buffer benötigt HEAD-Request-Support
- Streams laufen nur über Stream Relay (z.B. bei Standby)

**Konfiguration:**

1. **docker-compose.yml:**
```yaml
services:
  nginx-stream-proxy:
    image: nginx:alpine
    container_name: nginx-stream-proxy-livetest
    ports:
      - "17999:17999"
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf:ro

  xg2g:
    environment:
      - XG2G_STREAM_PORT=17999
      - XG2G_STREAM_BASE=http://10.10.55.193:17999  # Host-IP!
```

2. **nginx.conf:**
```nginx
http {
    server {
        listen 17999;

        location / {
            # HEAD-Requests direkt beantworten
            if ($request_method = HEAD) {
                add_header Content-Type "video/mp2t";
                return 200;
            }

            # GET-Requests an Enigma2 weiterleiten
            proxy_pass http://10.10.55.57:17999;
        }
    }
}
```

**Ergebnis:**
```
Stream-URL: http://10.10.55.193:17999/1:0:19:81:6:85:C00000:0:0:0:
Flow: Jellyfin → Threadfin → nginx (HEAD-Support) → Enigma2 (Port 17999)
```

---

## Wie erkennt xg2g welchen Modus nutzen?

**Priorität:**
1. ✅ Wenn `XG2G_STREAM_BASE` gesetzt → nutze diese URL (nginx-Proxy)
2. ✅ Sonst → nutze `XG2G_STREAM_PORT` mit Enigma2-Host (direkter Stream)

**Code-Logik:**
```go
// internal/openwebif/client.go
if streamBase := os.Getenv("XG2G_STREAM_BASE"); streamBase != "" {
    // Option B: nginx proxy
    return streamBase + "/" + ref
}
// Option A: direkter Stream
return enigma2Host + ":" + streamPort + "/" + ref
```

---

## Troubleshooting

### Problem: Threadfin zeigt "EOF" Fehler

**Ursache:** Enigma2 Stream Relay unterstützt keine HEAD-Requests

**Lösung:** Wechsel zu **Option B** (nginx-Proxy)

```bash
# docker-compose.yml anpassen
- XG2G_STREAM_BASE=http://<deine-host-ip>:17999

# nginx starten
docker compose up -d nginx-stream-proxy

# xg2g neu starten
docker compose restart xg2g
```

---

### Problem: nginx gibt "502 Bad Gateway"

**Ursache:** nginx kann Enigma2 nicht erreichen

**Lösung:** Prüfe nginx.conf Proxy-URL

```bash
# nginx logs prüfen
docker logs nginx-stream-proxy-livetest

# Verbindung testen
curl -I http://10.10.55.57:17999/test
```

---

## Empfehlung

Für **Production**:
- **Option A** (Port 8001) → Niedrigste Latenz, einfachstes Setup
- **Option B** (nginx) → Nur wenn Stream Relay zwingend nötig

Für **Testing/Development**:
- **Option B** (nginx) → Maximale Kompatibilität mit Threadfin
