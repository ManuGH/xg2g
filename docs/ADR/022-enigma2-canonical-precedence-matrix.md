# ADR-022: Enigma2 Canonical Precedence Matrix

**Status:** Accepted  
**Date:** 2026-02-11

## Context

Nach der Auflösung der alten `merge.go`-Monolithik existieren für Receiver-Konfiguration weiterhin zwei Namensräume:

- canonical: `enigma2.*` / `XG2G_E2_*`
- legacy compatibility: `openWebIF.*` / `XG2G_OWI_*` (plus ältere Einzel-Keys)

Das erzeugt ohne explizite Feldregeln Drift-Risiko und unklare Priorität.

## Decision

1. Canonical Quelle ist `enigma2.*` im File und `XG2G_E2_*` in ENV.
2. `openWebIF.*` und `XG2G_OWI_*` bleiben reine Fallback-Kompatibilität.
3. Globale Quellpriorität bleibt unverändert gemäß ADR-002: `ENV > File > Defaults`.
4. Innerhalb derselben Quelle gilt pro semantischem Feld: `canonical > legacy fallback`.
5. Wenn canonical und legacy gleichzeitig gesetzt sind und nach Normalisierung nicht äquivalent sind, ist das ein Konfigurationsfehler (fail closed).
6. Felder ohne Legacy-Alias sind canonical-only.

## Field Policy Matrix

| Runtime Field | Canonical File Key | Canonical ENV Key (Target) | Legacy File Fallback | Legacy ENV Fallback | Ziel-Policy |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `Enigma2.BaseURL` | `enigma2.baseUrl` | `XG2G_E2_HOST` | `openWebIF.baseUrl` | `XG2G_OWI_BASE` | canonical gewinnt, legacy nur Fallback |
| `Enigma2.Username` | `enigma2.username` | `XG2G_E2_USER` | `openWebIF.username` | `XG2G_OWI_USER` | canonical gewinnt, legacy nur Fallback |
| `Enigma2.Password` | `enigma2.password` | `XG2G_E2_PASS` | `openWebIF.password` | `XG2G_OWI_PASS` | canonical gewinnt, legacy nur Fallback |
| `Enigma2.Timeout` | `enigma2.timeout` | `XG2G_E2_TIMEOUT` | `openWebIF.timeout` | `XG2G_OWI_TIMEOUT_MS` | canonical gewinnt; Duration-Äquivalenz normalisiert |
| `Enigma2.Retries` | `enigma2.retries` | `XG2G_E2_RETRIES` | `openWebIF.retries` | `XG2G_OWI_RETRIES` | canonical gewinnt, legacy nur Fallback |
| `Enigma2.Backoff` | `enigma2.backoff` | `XG2G_E2_BACKOFF` | `openWebIF.backoff` | `XG2G_OWI_BACKOFF_MS` | canonical gewinnt; Duration-Äquivalenz normalisiert |
| `Enigma2.MaxBackoff` | `enigma2.maxBackoff` | `XG2G_E2_MAX_BACKOFF` | `openWebIF.maxBackoff` | `XG2G_OWI_MAX_BACKOFF_MS` | canonical gewinnt; Duration-Äquivalenz normalisiert |
| `Enigma2.StreamPort` (deprecated) | `enigma2.streamPort` | `XG2G_E2_STREAM_PORT` | `openWebIF.streamPort` | `XG2G_STREAM_PORT` | deprecated Feld: canonical gewinnt, legacy nur Übergang |
| `Enigma2.UseWebIFStreams` | `enigma2.useWebIFStreams` | `XG2G_E2_USE_WEBIF_STREAMS` | `openWebIF.useWebIFStreams` | `XG2G_USE_WEBIF_STREAMS` | canonical gewinnt, legacy nur Fallback |
| `Enigma2.ResponseHeaderTimeout` | `enigma2.responseHeaderTimeout` | `XG2G_E2_RESPONSE_HEADER_TIMEOUT` | - | - | canonical-only |
| `Enigma2.TuneTimeout` | `enigma2.tuneTimeout` | `XG2G_E2_TUNE_TIMEOUT` | - | - | canonical-only |
| `Enigma2.AuthMode` | `enigma2.authMode` | `XG2G_E2_AUTH_MODE` | - | - | canonical-only |
| `Enigma2.RateLimit` | `enigma2.rateLimit` | `XG2G_E2_RATE_LIMIT` | - | - | canonical-only |
| `Enigma2.RateBurst` | `enigma2.rateBurst` | `XG2G_E2_RATE_BURST` | - | - | canonical-only |
| `Enigma2.UserAgent` | `enigma2.userAgent` | `XG2G_E2_USER_AGENT` | - | - | canonical-only |
| `Enigma2.AnalyzeDuration` | `enigma2.analyzeDuration` | `XG2G_E2_ANALYZE_DURATION` | - | - | canonical-only |
| `Enigma2.ProbeSize` | `enigma2.probeSize` | `XG2G_E2_PROBE_SIZE` | - | - | canonical-only |
| `Enigma2.FallbackTo8001` | `enigma2.fallbackTo8001` | `XG2G_E2_FALLBACK_TO_8001` | - | - | canonical-only |
| `Enigma2.PreflightTimeout` | `enigma2.preflightTimeout` | `XG2G_E2_PREFLIGHT_TIMEOUT` | - | - | canonical-only |

## Consequences

- Neue Implementierungen müssen Alias-Pfade als reinen Fallback behandeln, nie als konkurrierende Primärquelle.
- Dokumentation/Schema/Registry sollen dieselbe Matrix reflektieren.
- Diese ADR trifft nur die Precedence-Entscheidung; Implementierungs- und Migrationsschritte folgen separat.
