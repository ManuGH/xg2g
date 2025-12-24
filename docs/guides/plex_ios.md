# Plex (iOS) Setup

## Warum das früher kompliziert war

iOS/Plex ist oft empfindlich bei Live-TV (MPEG-TS, Audio-Codecs, Header-Edgecases). xg2g löst das, indem Plex statt eines „rohen“ MPEG-TS-Streams einen kompatiblen HLS-Stream bekommt.

Wichtig: Das alte „Plex/iOS-Profil“ mit vielen `XG2G_PLEX_*` Tuning-Variablen existiert nicht mehr. Stattdessen gibt es eine vereinheitlichte HLS-Pipeline.

## Empfohlene Plex-Konfiguration

1. **HDHomeRun hinzufügen**
   - Plex → Live TV & DVR → Setup
   - Tuner-URL: `http://<xg2g-host>:8080`
2. **HLS für Plex erzwingen (besonders für iOS sinnvoll)**
   - `XG2G_PLEX_FORCE_HLS=true`
   - Dadurch liefert `GET /lineup.json` HLS-URLs an Plex aus.

## Relevante ENV Variablen

Minimal:

```bash
XG2G_PLEX_FORCE_HLS=true
XG2G_ENABLE_STREAM_PROXY=true
```

Kompatibilitäts-Defaults (meist sinnvoll):

```bash
XG2G_USE_RUST_REMUXER=true
XG2G_H264_STREAM_REPAIR=true
```

## Nützliche Hinweise

- HLS-URLs sehen typischerweise so aus: `/hls/<serviceRef>/playlist.m3u8`
- Der Stream-Proxy (Default `:18000`) übernimmt HLS-Segmentation und liefert **H.264 + AAC** aus.
