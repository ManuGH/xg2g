# Live Playback Attestation (Prod)

## Ziel
Live-Playback ist nur mit server-attestierter Entscheidung erlaubt (SSOT, fail-closed, multi-instance sicher).

## Pflicht-Konfiguration
Setze einen stabilen Secret ueber alle Instanzen:

- YAML: `api.playbackDecisionSecret`
- ENV: `XG2G_DECISION_SECRET`
- Empfehlung: 32+ Bytes random

Optional (empfohlen fuer Rotation):

- YAML: `api.playbackDecisionKeyId`
- ENV: `XG2G_PLAYBACK_DECISION_KID`
- YAML: `api.playbackDecisionPreviousKeys` (Format `kid:secret`)
- ENV: `XG2G_PLAYBACK_DECISION_PREVIOUS_KEYS` (comma-separated)
- YAML: `api.playbackDecisionRotationWindow`
- ENV: `XG2G_PLAYBACK_DECISION_ROTATION_WINDOW`

Beispiel:

- `openssl rand -hex 32`

## Semantik (Ist-Stand)
- `POST /api/v3/live/stream-info` liefert bei `mode != "deny"` ein `playbackDecisionToken`.
- `mode="deny"` ist kein Fehler, sondern ein regulaerer `200`-Response mit `mode: "deny"` und ohne `playbackDecisionToken`.
- `POST /api/v3/intents` erwartet fuer Live-Playback den root-level Request-Key `playbackDecisionToken` plus `params.playback_mode`.
- Backend toleriert die alten Params-Aliase `params.playback_decision_token` und `params.playback_decision_id` nur noch als Kompatibilitaets-Spiegel; sie duerfen den root-level Token nicht ersetzen oder ihm widersprechen.

## Live-Truth Problemvertrag
- `POST /api/v3/live/stream-info` kann `503 application/problem+json` fuer unbestaetigte Live-Medienwahrheit liefern.
- Stabile Problemtypen:
  - `/problems/live/scan_unavailable`
  - `/problems/live/missing_scan_truth`
  - `/problems/live/partial_truth`
  - `/problems/live/inactive_event_feed`
  - `/problems/live/failed_scan_truth`
- Stabile Retry-/Truth-Felder fuer diese Problemtypen:
  - `Retry-After`
  - `retryAfterSeconds`
  - `truthState`
  - `truthReason`
- Nur diagnostisch:
  - `truthOrigin`
  - `problemFlags`
- Clients muessen auf `type` verzweigen und duerfen `title`/`detail` nur als Fallback-Text behandeln.

## Intent-Watchpoint
- `/api/v3/intents` nutzt `scan.Capability` derzeit nur fuer Profilauflösung und Start-Parameter, nicht als zweite SSOT fuer Live-Container- oder Codec-Wahrheit.
- Der aktuelle Pfad bleibt unkritisch, solange er nur Signale wie `Interlaced` oder Profil-Hints konsumiert.
- Gefaehrliche kuenftige Aenderung: Wenn `/api/v3/intents` Container-, Codec- oder Readiness-Wahrheit direkt aus `scan.Capability` ableitet, muss derselbe Live-Truth-Resolver wie `/live/stream-info` verwendet werden. Kein zweiter impliziter Fallback-Pfad.

## Deployment-Check
1. Secret ist in allen Pods/Instanzen identisch.
2. Aktiver `kid` ist ueber alle Instanzen konsistent.
3. Bei Rotation: alte Keys als `previousKeys` hinterlegen und Window > 0 setzen.
4. Nach Window-Ende alte Keys entfernen.
5. Kein Warnlog wie:
   - `api.playbackDecisionSecret is not configured; using ephemeral ...`

## Smoke-Test (Happy Path)
1. `POST /api/v3/live/stream-info` mit `serviceRef` + `capabilities`.
2. Erwartung: `200`, `mode != "deny"`, Response enthaelt `playbackDecisionToken`.
3. `POST /api/v3/intents` mit:
   - `playbackDecisionToken` = `<playbackDecisionToken>`
   - `params.playback_mode` = `<mode aus stream-info>`
4. Erwartung: `202 Accepted` (Intent angelegt, Session-Playback kann starten).

## Negativtests (Reject-Semantik)
1. Root-level Token fehlt bei `/api/v3/intents`: HTTP `401`, Problem `code: "TOKEN_MISSING"`.
2. Token ungueltig: HTTP `401`, Problem `code` aus der Strict-JWT-Validierung.
3. Token-Claim-Mismatch (Principal/serviceRef/mode/capabilities): HTTP `403`, Problem `code: "CLAIM_MISMATCH"`.
4. Params-Alias vorhanden, aber nicht identisch zum root-level Token: HTTP `400`, Problem `code: "INVALID_INPUT"`.
5. Token-Erzeugung in `/api/v3/live/stream-info` nicht moeglich: HTTP `503`, Problem `code: "ATTESTATION_UNAVAILABLE"`.
6. `mode="deny"` aus `/live/stream-info`:
   - Frontend muss hart abbrechen (kein `/intents`), kein Token vorhanden.

## Verhalten ohne Secret
- Dev: funktioniert mit ephemerem Fallback-Key.
- Prod (mehrere Instanzen / Rolling): nicht zulaessig (false-deny-Risiko).

## Rotation (implementiert)
- Tokens tragen `kid` im Claims-Payload.
- Signierung nutzt den aktiven Key (`playbackDecisionSecret` + optional `playbackDecisionKeyId`).
- Verify akzeptiert aktive + previous keys bis `playbackDecisionRotationWindow` ablaeuft.
- Nach Ablauf des Windows werden previous keys deterministisch abgelehnt.

## Deprecation Policy: params aliases -> root-level `playbackDecisionToken`
Wir behandeln den root-level Request-Key `playbackDecisionToken` als kanonische Attestation-Sprache. `params.playback_decision_token` und `params.playback_decision_id` sind nur noch temporaere Kompatibilitaets-Aliase.

### Phase 1: Introduce + Document (no breaking change)
- OpenAPI dokumentiert `playbackDecisionToken` als kanonischen Request-Key.
- Backend verlangt den root-level Token fuer Live-Starts.
- Wenn Params-Aliase vorhanden sind und identisch zum root-level Token: akzeptiert.
- Wenn Params-Aliase vorhanden sind und unterschiedlich: deterministisch reject (`400`, `code: "INVALID_INPUT"`).

### Phase 2: Migrate + Measure (1-2 Releases)
- WebUI stellt vollstaendig auf root-level `playbackDecisionToken` um.
- Server misst Alias-Nutzung serverseitig (Counter/Log inkl. Request-ID), um Removal datenbasiert zu entscheiden.
- Merge-Gates bleiben aktiv: OpenAPI normative snapshot, UI-Consumption-Verify, Contract-Tests.

### Phase 3: Remove (breaking change)
- Params-Aliase werden entfernt.
- Requests mit `params.playback_decision_token` oder `params.playback_decision_id` werden deterministisch rejected (`400`, `code: "INVALID_INPUT"`) mit klarer Fehlermeldung und Migrationshinweis.
- Removal erfolgt nur, wenn Alias-Nutzung im Beobachtungsfenster (z. B. 14 Tage) unter Schwellwert liegt (z. B. <0.1% der `/intents` Requests).

## Telemetrie-Schnitt (normiert)
Empfohlene Counter fuer Phase-2-Entscheidungen:
- `xg2g_live_intents_playback_key_total{key="request|request+playback_decision_token|request+playback_decision_id|all|params_only|none",result="accepted|equal|mismatch|rejected_missing"}`
- `xg2g_live_intents_total{path="/api/v3/intents",type="stream.start"}`

Alias-Quote fuer Removal-Gate:
- `rate(xg2g_live_intents_playback_key_total{key="request+playback_decision_id",result="equal"}[14d]) / rate(xg2g_live_intents_total{path="/api/v3/intents",type="stream.start"}[14d]) < 0.001`
