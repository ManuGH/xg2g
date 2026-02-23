# Live Playback Attestation (Prod)

## Ziel
Live-Playback ist nur mit server-attestierter Entscheidung erlaubt (SSOT, fail-closed, multi-instance sicher).

## Pflicht-Konfiguration
Setze einen stabilen Secret über alle Instanzen:

- YAML: `api.playbackDecisionSecret`
- ENV: `XG2G_PLAYBACK_DECISION_SECRET`
- Empfehlung: 32+ Bytes random

Beispiel:

- `openssl rand -hex 32`

## Semantik (Ist-Stand)
- `POST /api/v3/live/stream-info` liefert bei `mode != "deny"` ein `playbackDecisionToken`.
- `mode="deny"` ist kein Fehler, sondern ein regulaerer `200`-Response mit `mode: "deny"` und ohne `playbackDecisionToken`.
- `POST /api/v3/intents` akzeptiert Live-Playback nur mit `params.playback_mode` und `params.playback_decision_token` (kanonisch im Client; enthaelt Token-Wert).
- Backend akzeptiert zusaetzlich Alias `params.playback_decision_id`.

## Deployment-Check
1. Secret ist in allen Pods/Instanzen identisch.
2. Rolling Deploy ohne Secret-Wechsel.
3. Kein Warnlog wie:
   - `api.playbackDecisionSecret is not configured; using ephemeral ...`

## Smoke-Test (Happy Path)
1. `POST /api/v3/live/stream-info` mit `serviceRef` + `capabilities`.
2. Erwartung: `200`, `mode != "deny"`, Response enthaelt `playbackDecisionToken`.
3. `POST /api/v3/intents` mit:
   - `params.playback_mode` = `<mode aus stream-info>`
   - `params.playback_decision_token` = `<playbackDecisionToken>`
4. Erwartung: `202 Accepted` (Intent angelegt, Session-Playback kann starten).

## Negativtests (Reject-Semantik)
1. Token fehlt/ungueltig/mismatch (Principal/serviceRef/mode) bei `/api/v3/intents`: HTTP `400`, Problem `code: "INVALID_INPUT"`.
2. Token-Erzeugung in `/api/v3/live/stream-info` nicht moeglich: HTTP `503`, Problem `code: "ATTESTATION_UNAVAILABLE"`.
3. `mode="deny"` aus `/live/stream-info`:
   - Frontend muss hart abbrechen (kein `/intents`), kein Token vorhanden.

## Verhalten ohne Secret
- Dev: funktioniert mit ephemerem Fallback-Key.
- Prod (mehrere Instanzen / Rolling): nicht zulaessig (false-deny-Risiko).

## Rotation (optional)
- Spaeter `kid` + Multi-Key-Verify (current + previous key).
- Erst dann Zero-Downtime-Key-Rotation aktivieren.

## Deprecation Policy: `playback_decision_id` -> `playback_decision_token`
Wir behandeln `params.playback_decision_token` als kanonischen Request-Key und `params.playback_decision_id` nur als temporaeren Kompatibilitaets-Alias.

### Phase 1: Introduce + Document (no breaking change)
- OpenAPI dokumentiert `playback_decision_token` als Standard und markiert `playback_decision_id` explizit als deprecated.
- Backend akzeptiert beide Keys.
- Wenn beide vorhanden sind und identisch: `playback_decision_token` gewinnt.
- Wenn beide vorhanden sind und unterschiedlich: deterministisch reject (`400`, `code: "INVALID_INPUT"`).

### Phase 2: Migrate + Measure (1-2 Releases)
- WebUI stellt vollstaendig auf `playback_decision_token` um.
- Server misst Alias-Nutzung (`playback_decision_id`) serverseitig (Counter/Log inkl. Request-ID), um Removal datenbasiert zu entscheiden.
- Merge-Gates bleiben aktiv: OpenAPI normative snapshot, UI-Consumption-Verify, Contract-Tests (bis Removal: beide Keys; danach: nur canonical + reject alias).

### Phase 3: Remove (breaking change)
- Alias wird entfernt.
- Requests mit `playback_decision_id` werden deterministisch rejected (`400`, `code: "INVALID_INPUT"`) mit klarer Fehlermeldung und Migrationshinweis.
- Removal erfolgt nur, wenn Alias-Nutzung im Beobachtungsfenster (z. B. 14 Tage) unter Schwellwert liegt (z. B. <0.1% der `/intents` Requests).

## Telemetrie-Schnitt (normiert)
Empfohlene Counter fuer Phase-2-Entscheidungen:
- `xg2g_live_intents_playback_key_total{key="playback_decision_token|playback_decision_id|both|none",result="accepted|equal|mismatch|rejected_missing|rejected_invalid"}`
- `xg2g_live_intents_total{path="/api/v3/intents",type="stream.start"}`

Alias-Quote fuer Removal-Gate:
- `rate(xg2g_live_intents_playback_key_total{key="playback_decision_id",result="accepted"}[14d]) / rate(xg2g_live_intents_total{path="/api/v3/intents",type="stream.start"}[14d]) < 0.001`
