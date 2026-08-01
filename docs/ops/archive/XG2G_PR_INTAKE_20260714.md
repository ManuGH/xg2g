# xg2g PR Intake vor dem finalen Refactor-Test

Stand: 2026-07-14  
Referenz-Branch: `feat/modern-ui-planner-safe`

Dieses Dokument verhindert, dass offene PRs pauschal in den PlaybackPlanner-Refactor
cherry-picked werden. Jede Änderung muss gegen den aktuellen Stand geprüft werden.

## Offene PRs

| PR | Entscheidung | Status |
|---|---|---|
| #665 `actions/setup-node` | Separat mergen, nicht Bestandteil des Refactors | alle Checks grün |
| #664 npm-Dependencies | Zurückhalten; erst nach eigener CI-/Trivy-Reparatur prüfen | Checks fehlgeschlagen |
| #662 UI Sidebar/Player | Nicht als PR mergen; Quelländerungen sind lokal vorhanden, `dist` später neu erzeugen | offen, Checks grün |
| #663 Reconcile-Sammel-PR | Nicht komplett mergen; nur einzelne fachliche Änderungen prüfen | CI-Gate fehlgeschlagen |

## Bereits im Refactor vorhanden

Die folgenden fachlichen Änderungen aus #663 wurden im aktuellen Code bereits
gefunden und dürfen nicht erneut übernommen werden:

- HLS- und `/dev/shm`-Pfadauflösung
- 20M-FFmpeg-Probe und Audio-Synchronisierung mit `aresample=async=1`
- Pfad-Traversal-/Symlink-Schutz
- Safari/iOS-4K-Fallback mit `3840x2160`
- Planner-Ratenlimits und Target-Profile

Der alte 1080p-Cap aus #663 (`30ea29ac`) ist ausdrücklich abzulehnen, weil er
dem korrigierten 4K-Fallback widerspricht.

## Selektive Prüfung von #663

Nur wenn ein Verhalten im aktuellen Code oder in den Charakterisierungstests
fehlt, darf daraus ein eigener, kleiner Commit entstehen. Zulässige Kandidaten:

1. Source-Truth-/Channel-Topology-Freshness und Fast-Probe-Cache
2. HLS-Startup-Verhalten (`EXT-X-START`, Live-Headroom)
3. FFmpeg-Probe-/Audio-Semantik, jeweils mit Regressionstest
4. Sicherheitskorrekturen, falls der bestehende Schutz nicht gleichwertig ist

Jeder Kandidat wird als `already-present`, `port-selectively` oder `rejected`
klassifiziert. Kein Sammel-Cherry-Pick von #663.

## Finales Gate

Vor einem Cutover müssen folgende Schritte in dieser Reihenfolge erfolgreich sein:

1. Arbeitsbaum für den Test-Checkpoint separat einfrieren; bestehende UI-/Android-
   Änderungen nicht mit dem Refactor vermischen.
2. `go test ./...`
3. `go test -race ./...`
4. WebUI-Lint und reproduzierbarer UI-Build; danach generierte `dist`-Assets prüfen.
5. Staging-Deployment ausschließlich nach LXC 110 auf `:8089`.
6. Playback-Charakterisierung und Shadow-Gate erneut ausführen.
7. Erst nach manueller Freigabe Promotion auf `:8088`.

Offene PRs gelten nicht als integriert, solange kein eigener Commit bzw. Merge
auf dem Refactor-Checkpoint nachweisbar ist.
