# Plex/iOS-Profil für xg2g

## Überblick

Das Plex/iOS-Profil ist eine optimierte Streaming-Lösung für **Plex Media Server** und **iOS-Geräte** (iPhone, iPad). Es löst die häufigsten Probleme mit Live-TV-Streaming auf Plex:

- ✅ **Kein Transcoding**: Direct Play auf iPhone/iPad ohne Serverbelastung
- ✅ **Schneller Start**: Pre-Buffering von 1-2 Segmenten für sofortiges Abspielen
- ✅ **Stabile Streams**: Kurze HLS-Segmente (2-4 Sekunden) verhindern Timeouts
- ✅ **Plex-Kompatibilität**: H.264 mit korrekten PPS/SPS-Headern (h264_mp4toannexb)
- ✅ **iOS-Native Codecs**: H.264/AVC + AAC-LC für hardwarebeschleunigte Wiedergabe

## Wie es funktioniert

### Automatische Aktivierung

Das Plex-Profil wird **automatisch aktiviert**, wenn xg2g einen Plex-Client erkennt:

```
User-Agent: Plex Media Server/...
User-Agent: PlexiOS/...
```

Wenn ein Plex-Client einen Stream anfordert, passiert Folgendes:

1. **User-Agent-Erkennung**: xg2g erkennt Plex-Clients automatisch
2. **HLS-Umleitung**: Der Request wird zu `/hls/<service_ref>` umgeleitet
3. **Plex-Profil-Start**: Ein optimiertes HLS-Profil wird erstellt:
   - **Video**: H.264 copy mit `h264_mp4toannexb` Bitstream-Filter
   - **Audio**: AAC-LC Transcoding (192k Stereo)
   - **Container**: HLS mit 2-Sekunden-Segmenten
4. **Pre-Buffering**: 2 Segmente werden vorproduziert (ca. 4 Sekunden)
5. **Playlist-Auslieferung**: `.m3u8` Playlist mit korrekten Content-Types
6. **Segment-Streaming**: `.ts` Segmente mit `video/mp2t` Content-Type

### Unterschied zu Standard-HLS

| Feature | Standard HLS | Plex-Profil |
|---------|--------------|-------------|
| Segment-Dauer | 2s | 2s (konfigurierbar) |
| Playlist-Größe | 6 Segmente | 3 Segmente (schnellerer Start) |
| Pre-Buffering | Nein | Ja (2 Segmente) |
| H.264 Repair | Nein | Ja (`h264_mp4toannexb`) |
| AAC Transcoding | Optional | Standard (iOS-Kompatibilität) |
| `program_date_time` | Nein | Ja (Plex-Anforderung) |

## Konfiguration

### Umgebungsvariablen

```bash
# Plex-Profil-Konfiguration
XG2G_PLEX_PROFILE_ENABLED=true         # Aktiviert Plex-Profil (Standard: auto bei Plex User-Agent)
XG2G_PLEX_SEGMENT_DURATION=2           # HLS-Segment-Dauer in Sekunden (2-10, Standard: 2)
XG2G_PLEX_PLAYLIST_SIZE=3              # Anzahl Segmente in Playlist (3-10, Standard: 3)
XG2G_PLEX_STARTUP_SEGMENTS=2           # Pre-Buffering: Anzahl Segmente vor Start (1-5, Standard: 2)
XG2G_PLEX_FORCE_AAC=true               # Audio-Transcoding zu AAC erzwingen (Standard: true)
XG2G_PLEX_AAC_BITRATE=192k             # AAC-Bitrate (Standard: 192k)
XG2G_PLEX_FFMPEG_PATH=/usr/bin/ffmpeg  # Pfad zu ffmpeg (Standard: /usr/bin/ffmpeg)
```

### Beispiel: Docker Compose

```yaml
version: '3.8'

services:
  xg2g:
    image: ghcr.io/manuGH/xg2g:latest
    container_name: xg2g
    ports:
      - "8080:8080"    # API/WebUI
      - "18000:18000"  # Stream-Proxy (Plex verbindet hierher)
    environment:
      # Receiver-Konfiguration
      XG2G_OWI_BASE: "http://10.10.55.64"
      XG2G_BOUQUET: "Premium,Favourites"

      # Stream-Proxy aktivieren
      XG2G_ENABLE_STREAM_PROXY: "true"
      XG2G_PROXY_PORT: "18000"

      # Plex/iOS-Profil (optimierte Einstellungen)
      XG2G_PLEX_PROFILE_ENABLED: "true"
      XG2G_PLEX_SEGMENT_DURATION: "2"    # Kurze Segmente für schnellen Start
      XG2G_PLEX_PLAYLIST_SIZE: "3"       # Kleine Playlist = weniger Latenz
      XG2G_PLEX_STARTUP_SEGMENTS: "2"    # 2 Segmente vorproduzieren (4s Puffer)
      XG2G_PLEX_FORCE_AAC: "true"        # AAC für iOS
      XG2G_PLEX_AAC_BITRATE: "192k"      # Hohe Qualität für Live-TV

      # H.264 Stream Repair (bereits standardmäßig aktiviert)
      XG2G_H264_STREAM_REPAIR: "true"

      # Optionale Optimierungen
      XG2G_LOG_LEVEL: "info"
    volumes:
      - ./data:/data
    restart: unless-stopped
```

### Plex Media Server Einrichtung

1. **HDHomeRun-Tuner hinzufügen**:
   - Plex Settings → Live TV & DVR → Set Up Plex DVR
   - Wähle "HDHomeRun" als Tuner-Typ
   - Gib die xg2g-URL ein: `http://<xg2g-ip>:8080`
   - Plex erkennt xg2g automatisch als HDHomeRun-Tuner

2. **EPG zuordnen**:
   - Plex lädt automatisch die XMLTV-EPG von `http://<xg2g-ip>:8080/xmltv.xml`
   - Ordne Kanäle manuell zu, falls nötig

3. **Stream-URLs**:
   - Plex ruft Streams von Port **18000** ab (Stream-Proxy)
   - xg2g erkennt den Plex User-Agent und aktiviert automatisch das Plex-Profil

## Technische Details

### FFmpeg-Kommando (Plex-Profil)

Das Plex-Profil verwendet folgendes FFmpeg-Kommando:

```bash
ffmpeg \
  -hide_banner -loglevel warning \
  -fflags +genpts+igndts \              # Timestamps regenerieren (Enigma2 fix)
  -i http://receiver:8001/1:0:19:... \  # Stream-Quelle
  -map 0:v -c:v copy \                   # Video kopieren (kein Re-Encoding)
  -bsf:v h264_mp4toannexb \              # PPS/SPS-Header hinzufügen (Plex-Fix)
  -map 0:a -c:a aac \                    # Audio zu AAC transcodieren
  -b:a 192k -ac 2 -async 1 \             # AAC-Stereo mit 192k
  -start_at_zero \                       # Timestamps bei 0 starten
  -avoid_negative_ts make_zero \         # Negative Timestamps korrigieren
  -muxdelay 0 -muxpreload 0 \            # Keine Mux-Verzögerung
  -mpegts_copyts 1 \                     # Timestamps in MPEG-TS übernehmen
  -mpegts_flags resend_headers+initial_discontinuity \
  -pcr_period 20 \                       # PCR alle 20ms
  -pat_period 0.1 \                      # PAT alle 100ms
  -sdt_period 0.5 \                      # SDT alle 500ms
  -f hls \                               # HLS-Ausgabe
  -hls_time 2 \                          # 2-Sekunden-Segmente
  -hls_list_size 3 \                     # 3 Segmente in Playlist
  -hls_flags delete_segments+append_list+program_date_time \
  -hls_segment_type mpegts \             # MPEG-TS Segmente
  -hls_segment_filename segment_%03d.ts \
  playlist.m3u8
```

### Warum `h264_mp4toannexb`?

Enigma2-Receiver liefern H.264-Streams oft **ohne korrekte PPS/SPS-Header** (Picture Parameter Set / Sequence Parameter Set). Diese Header sind aber **zwingend notwendig** für:

- **Plex Direct Play**: Ohne PPS/SPS kann Plex den Stream nicht dekodieren
- **iOS Hardware-Decoding**: iOS-Geräte erwarten Annex-B-Format
- **Schnellen Stream-Start**: I-Frames können ohne SPS nicht dekodiert werden

Der `h264_mp4toannexb` Bitstream-Filter:
- Extrahiert PPS/SPS aus dem Stream-Kontext
- Fügt sie als NAL Units in jeden Keyframe ein
- Konvertiert zu Annex-B-Format (Standard für Broadcast-Streams)

**Performance**: Zero-Copy Operation, kein Re-Encoding → **~10-20ms Latenz**

### Pre-Buffering-Strategie

Ohne Pre-Buffering:
```
Plex Request → FFmpeg Start → Tuning (2-5s) → Erste Segmente → Playlist → Client
                ❌ Timeout nach 10s
```

Mit Pre-Buffering (Plex-Profil):
```
Plex Request → FFmpeg Start → Tuning (2-5s) → Warte auf 2 Segmente → Playlist bereit → Client
                                                ✅ 4 Sekunden Puffer vorhanden
```

Vorteile:
- **Kein Timeout**: Playlist enthält bereits abspielbare Segmente
- **Sofortiger Start**: Client kann direkt abspielen (kein Buffering-Spinner)
- **Bessere Fehlertoleranz**: 4 Sekunden Puffer überbrücken kurze Netzwerk-Störungen

## Performance

### Ressourcenverbrauch

| Komponente | CPU | RAM | Latenz |
|------------|-----|-----|--------|
| H.264 Bitstream-Filter | <0.1% | ~1 MB | ~10-20 ms |
| AAC-Transcoding (FFmpeg) | ~5-10% | ~10 MB | ~200-500 ms |
| HLS-Segmentierung | <1% | ~5 MB | ~100 ms |
| **Gesamt pro Stream** | **~10-15%** | **~20 MB** | **~500 ms** |

**Hinweis**: Bei Hardware-Transcoding (GPU) reduziert sich die CPU-Last auf <1%.

### Vergleich: Plex-Profil vs. Server-Transcoding

| Metrik | Plex-Profil (xg2g) | Plex Server Transcoding |
|--------|--------------------|-----------------------|
| CPU-Last | ~10% | ~50-100% |
| RAM | ~20 MB | ~200-500 MB |
| Start-Latenz | ~5 Sekunden | ~10-30 Sekunden |
| Qualität | Original (H.264 copy) | Re-Encoding-Artefakte |
| Netzwerk | ~3-8 Mbit/s | Variable (Plex-Profil) |

## Fehlerbehebung

### Problem: Plex zeigt "Playback Error"

**Ursache**: H.264-Stream ohne PPS/SPS-Header

**Lösung**:
```bash
# Prüfen, ob H.264 Repair aktiviert ist
docker logs xg2g | grep "H.264 stream repair"

# Falls nicht, aktivieren:
XG2G_H264_STREAM_REPAIR=true
```

### Problem: "Buffering..." auf iPhone

**Ursache 1**: Segmente zu lang (>4s)

**Lösung**:
```bash
XG2G_PLEX_SEGMENT_DURATION=2  # Auf 2 Sekunden reduzieren
```

**Ursache 2**: AAC-Transcoding zu langsam

**Lösung**:
```bash
XG2G_PLEX_AAC_BITRATE=128k    # Bitrate reduzieren
# ODER: Rust-Remuxer aktivieren (schneller)
XG2G_USE_RUST_REMUXER=true
```

### Problem: Stream startet nicht (Timeout)

**Ursache**: Pre-Buffering-Timeout zu kurz für langsamen Tuner

**Lösung**:
```bash
XG2G_PLEX_STARTUP_SEGMENTS=1  # Nur 1 Segment vorproduzieren
# ODER: Segment-Dauer erhöhen
XG2G_PLEX_SEGMENT_DURATION=4  # 4 Sekunden (2 Segmente = 8s Puffer)
```

### Problem: Audio-Sync-Probleme

**Ursache**: Enigma2 liefert gebrochene DTS-Timestamps

**Lösung**: Bereits im Plex-Profil integriert via `-fflags +genpts+igndts`

Falls trotzdem Probleme:
```bash
# Logs prüfen
docker logs xg2g | grep "ffmpeg"

# Audio-Transcoding deaktivieren (Test)
XG2G_PLEX_FORCE_AAC=false
```

## Logs & Debugging

### Log-Einträge prüfen

```bash
# Plex-Client-Erkennung
docker logs xg2g | grep "auto-redirecting Plex client"

# Plex-Profil-Start
docker logs xg2g | grep "starting Plex/iOS HLS profile"

# Segment-Produktion
docker logs xg2g | grep "initial segments ready"

# FFmpeg-Fehler
docker logs xg2g | grep "plex profile ffmpeg"
```

### Debug-Modus aktivieren

```bash
XG2G_LOG_LEVEL=debug
```

Beispiel-Log (erfolgreicher Stream):
```
INF auto-redirecting Plex client to optimized HLS profile user_agent="Plex Media Server/1.32.5.7516" original_path="/1:0:19:132F:3EF:1:C00000:0:0:0:"
INF starting Plex/iOS HLS profile target="http://10.10.55.64:8001/1:0:19:132F:3EF:1:C00000:0:0:0:" segment_duration=2 playlist_size=3 force_aac=true
INF initial segments ready segments=2 min_required=2
DBG serving Plex playlist service_ref="1:0:19:132F:3EF:1:C00000:0:0:0:" content_type="application/vnd.apple.mpegurl"
```

## Best Practices

### Empfohlene Einstellungen

**Für schnelles Zapping** (Kanalwechsel):
```bash
XG2G_PLEX_SEGMENT_DURATION=2
XG2G_PLEX_PLAYLIST_SIZE=3
XG2G_PLEX_STARTUP_SEGMENTS=1  # Nur 1 Segment = ~2s Start
```

**Für stabile Streams** (wenig Buffering):
```bash
XG2G_PLEX_SEGMENT_DURATION=4
XG2G_PLEX_PLAYLIST_SIZE=6
XG2G_PLEX_STARTUP_SEGMENTS=2  # 2 Segmente = ~8s Puffer
```

**Für schwache Server** (Raspberry Pi):
```bash
XG2G_PLEX_AAC_BITRATE=128k      # Niedrige Bitrate
XG2G_USE_RUST_REMUXER=true      # Rust statt FFmpeg (schneller)
XG2G_PLEX_STARTUP_SEGMENTS=1    # Weniger Vorproduktion
```

### Netzwerk-Optimierung

**LAN/WLAN** (hohe Bandbreite):
```bash
XG2G_PLEX_AAC_BITRATE=192k      # Beste Qualität
XG2G_PLEX_SEGMENT_DURATION=2    # Schneller Start
```

**Remote/VPN** (begrenzte Bandbreite):
```bash
XG2G_PLEX_AAC_BITRATE=128k      # Reduzierte Bitrate
XG2G_PLEX_SEGMENT_DURATION=4    # Größere Segmente = weniger Overhead
```

## Kompatibilität

### Getestet mit

| Client | Version | Status | Notizen |
|--------|---------|--------|---------|
| Plex for iOS | 8.x+ | ✅ | Direct Play, kein Transcoding |
| Plex for Android | 9.x+ | ✅ | Direct Play |
| Plex Web | Latest | ✅ | HLS-Wiedergabe |
| Plex for Apple TV | 8.x+ | ✅ | Hardware-Decoding |
| Plex for Smart TV | Varies | ⚠️ | Hängt von TV-Codec-Unterstützung ab |

### Codec-Anforderungen

**Client muss unterstützen**:
- H.264/AVC (Level 4.0+)
- AAC-LC Audio
- HLS (HTTP Live Streaming)

**Nicht kompatibel**:
- Sehr alte Android-Geräte (<4.4)
- Clients ohne HLS-Unterstützung
- Browser ohne Media Source Extensions (MSE)

## Vergleich zu Alternativen

### xg2g Plex-Profil vs. xTeVe

| Feature | xg2g Plex-Profil | xTeVe |
|---------|------------------|-------|
| H.264 Repair | ✅ Integriert | ⚠️ Manuell via FFmpeg-Buffer |
| HLS für Plex | ✅ Automatisch | ⚠️ Konfiguration nötig |
| Pre-Buffering | ✅ Ja | ❌ Nein |
| User-Agent-Routing | ✅ Ja | ❌ Nein |
| Rust-Remuxer | ✅ Ja | ❌ Nein |
| Komplexität | ✅ Einfach | ⚠️ Komplex |

### xg2g Plex-Profil vs. ErsatzTV

| Feature | xg2g Plex-Profil | ErsatzTV |
|---------|------------------|----------|
| Live-TV | ✅ Ja | ❌ Nein (nur VOD) |
| Hardware-Transcoding | ⚠️ Optional | ✅ Ja |
| Komplexität | ✅ Einfach | ⚠️ Komplex |
| Ressourcen | ✅ Niedrig | ⚠️ Hoch |

## Weiterführende Links

- [Plex Live TV Requirements](https://support.plex.tv/articles/225877427-supported-dvr-tuners-and-antennas/)
- [HLS Specification (RFC 8216)](https://tools.ietf.org/html/rfc8216)
- [H.264 Annex-B Format](https://yumichan.net/video-processing/video-compression/introduction-to-h264-nal-unit/)
- [FFmpeg HLS Documentation](https://ffmpeg.org/ffmpeg-formats.html#hls-2)

## Lizenz

MIT License - siehe [LICENSE](../LICENSE)
