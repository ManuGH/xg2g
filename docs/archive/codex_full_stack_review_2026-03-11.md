# xg2g Full-Stack Review

| Scope | Value |
| --- | --- |
| Repo | `/root/xg2g` |
| Go workspace under review | `backend/` |
| Audit date | 2026-03-11 |
| Threat model | Netzwerkzugang ins Home-Lab (kompromittiertes IoT-Gerät / Nachbar im WLAN) |

## Phase 1: Security Audit

| ID | CVSS | Vector | Datei:Zeile | Finding | Exploitation | Fix |
| --- | --- | --- | --- | --- | --- | --- |
| SEC-01 | 8.8 | AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:N | `backend/internal/config/registry.go:130`<br>`backend/internal/config/registry.go:183`<br>`backend/internal/config/registry.go:186`<br>`backend/internal/daemon/manager.go:171`<br>`backend/internal/control/http/v3/auth.go:129`<br>`infrastructure/docker/docker-compose.yml:7` | API bindet standardmaessig auf `:8088`, TLS und `ForceHTTPS` sind default-off, der Daemon serviert dann Klartext-HTTP, und Session-Cookies werden ohne `Secure` gesetzt. | Ein Angreifer im LAN kann Bearer-Token oder `xg2g_session`-Cookies sniffen und mit denselben Scopes wiederverwenden. | Auf `127.0.0.1` defaulten oder TLS default-on machen; Start verweigern, wenn Auth auf non-loopback ohne TLS/trusted proxy aktiviert ist; Session-Erstellung auf Klartext-HTTP hart ablehnen. |
| SEC-02 | 8.8 | AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H | `.env:3`<br>`.env:4`<br>`.env:15`<br>`backend/config.yaml:9`<br>`backend/config.yaml:17`<br>`infrastructure/docker/docker-compose.dev.yml:6` | Getrackte Live-Credentials liegen im Repo: OpenWebIF-Passwort `Kiddy99`, API-Token `dev` mit `v3:admin,v3:read,v3:write`, plus statischer Decision-Secret. Die Dev-Compose-Datei konsumiert `./.env` direkt. | Wer eine Dev-Instanz mit der Repo-`.env` startet, stellt berechenbare Zugangsdaten ins Netz. Ein LAN-Angreifer muss dann nur `Bearer dev` probieren. | `.env` aus Git entfernen, Secrets rotieren, `.env` gitignoren, Dev-Defaults ohne echte Werte halten, Start verweigern wenn schwache Platzhalter-Tokens erkannt werden. |
| SEC-03 | 6.5 | AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N | `.gitleaks.toml:4`<br>`.github/workflows/container-security.yml:29`<br>`.env:3` | Secret-Scanning verhindert den Commit von Secrets nicht. Die Custom-`gitleaks`-Config enthaelt nur Allowlist-Eintraege, waehrend getrackte Secrets im Tree liegen. | Inferenz aus Tree + CI: Die Secret-Gate deckt den realen Angriffsfall nicht ab; dieselben Klartext-Secrets koennen erneut committed werden. | `gitleaks` explizit mit Default-Regeln erweitern (`extend`/`useDefault`) oder eigene Regeln definieren; CI bei Secret-Funden failen; `.env` und reale Configs aus dem Repo entfernen. |
| SUP-01 | 7.5 | AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:L | `backend/go.mod:5` | Toolchain liegt hinter Security-Fixes. `go.mod` pinnt `toolchain go1.25.7`, `govulncheck` meldet erreichbare Stdlib-Vulns, deren Fix u.a. `go1.25.8` erfordert (`GO-2026-4601/4602/4603`). Im Audit lief lokal sogar `go1.25.0`. | URL-/Template-/Filesystem-Pfade laufen auf einer verwundbaren Stdlib-Basis; lokal ist der Befund noch schlechter als der Repo-Pin. | Toolchain auf `go1.25.8+` anheben, lokale Builds mit `GOTOOLCHAIN=auto` erzwingen, `go version` in CI und Release hart validieren. |
| SUP-02 | 7.7 | AV:N/AC:L/PR:N/UI:N/S:C/C:H/I:H/A:H | `.github/workflows/release.yml:18`<br>`.github/workflows/release.yml:28`<br>`.github/workflows/release.yml:41` | Release-Pipeline ist supply-chain-weich: mutable Action-Tags (`@v4/@v5/@v6`) und direkter `curl | tar`-Download von `git-cliff` ohne Hash-/Signatur-Pruefung. | Ein kompromittiertes Release-Asset oder retaggte Action kann direkt in den Release-Job und damit in ausgelieferte Artefakte injizieren. | Alle Actions auf Commit-SHAs pinnen; Fremdbinaries nur mit SHA256/Sigstore/attestation verifizieren; unnoetige Download-Schritte eliminieren. |
| SUP-03 | 5.3 | AV:N/AC:L/PR:N/UI:N/S:U/C:L/I:L/A:N | `.github/workflows/lint.yml:1`<br>`.github/workflows/phase4-guardrails.yml:1`<br>`.github/workflows/repo-health.yml:1`<br>`.github/workflows/trivy.yml:76`<br>`.github/workflows/trivy.yml:126` | Drei Workflows deklarieren gar keine expliziten `permissions`, und Trivy-Enforcement ist standardmaessig deaktiviert. | Bei zu breiten Repo-Defaults laeuft Job-Code mit mehr `GITHUB_TOKEN`-Rechten als noetig; Sicherheits-Scans bleiben informational. | Top-level `permissions: { contents: read }` fuer alle Workflows setzen, nur job-lokal erweitern, Trivy auf geschuetzten Branches zwingend failen lassen. |
| SEC-04 | 5.9 | AV:N/AC:L/PR:L/UI:N/S:U/C:H/I:L/A:L | `backend/internal/control/http/v3/handlers_intents.go:81`<br>`backend/internal/platform/net/outbound.go:79`<br>`backend/internal/infra/media/ffmpeg/plan_builder.go:205` | Direkte `http/https`-Quellen werden allowlist-geprueft, danach aber als nacktes `-i <url>` an FFmpeg uebergeben. Ein `-protocol_whitelist` fehlt komplett. | Ein erlaubter, aber kompromittierter Upstream kann Redirects/Playlists liefern, die unerwuenschte FFmpeg-Protokolle oder Demuxer triggern. | Fuer `SourceURL` und HTTP-basierte Tuner-Inputs einen minimalen `-protocol_whitelist` setzen und Redirect-/Demuxer-Pfade explizit testen. |

## Phase 2: Architektur & Concurrency

| ID | Severity | Component | Datei:Zeile | Finding | Fix | Effort |
| --- | --- | --- | --- | --- | --- | --- |
| ARC-01 | HIGH | `control/http/v3` | `backend/internal/control/http/v3/handlers_intents.go:205`<br>`backend/internal/control/http/v3/intents/service.go:43`<br>`backend/internal/control/http/v3/auth_strict_test.go:688` | Der Start-Intent-Flow ist doppelt implementiert: einmal im HTTP-Handler, einmal im `intents.Service`. Die Kopie driftet bereits; `go test ./...` bricht im Race-Safety-Kontrakt fuer Phase-2-Idempotenz. | Handler-Logik loeschen und exakt eine Transport-zu-Domain-Uebersetzung behalten: HTTP -> `Intent` -> `intents.Service.ProcessIntent()` -> HTTP-Response-Mapping. Replay-Helper nur einmal halten. | M |
| CFG-02 | MEDIUM | `config/runtime` | `backend/internal/config/env.go:23`<br>`backend/internal/infra/media/ffmpeg/adapter.go:106`<br>`backend/internal/pipeline/profiles/resolve.go:111`<br>`.github/workflows/lint.yml:25` | Produktionscode liest weiter live Umgebungsvariablen ueber `config.Parse*`. Der Guardrail-Workflow prueft nur rohe `os.Getenv`/`os.LookupEnv`-Aufrufe und verfehlt damit den echten Escape-Hatch. | Alle Laufzeit-Tunables in `AppConfig`/Snapshot heben; `config.Parse*` ausserhalb `internal/config` und Bootstrap verbieten; Lint-Workflow auf diese API erweitern. | M |
| CON-04 | LOW | `pipeline/scan` | `backend/internal/pipeline/scan/manager.go:203` | Der Scan-Manager faellt auf einen detached `context.TODO()` zurueck, wenn `AttachLifecycle()` vergessen wurde. Das ist versteckte Laufzeitkopplung statt fail-closed. | `RunBackground()` ohne gebundenen Lifecycle hart ablehnen oder Parent-Context im Konstruktor verpflichtend machen. | S |

## Phase 3: Transcoding Pipeline

| ID | Severity | Symptom | Datei:Zeile | Root Cause | Fix | Test-Strategie |
| --- | --- | --- | --- | --- | --- | --- |
| PIPE-01 | MEDIUM | Erlaubte Remote-Quelle kann FFmpeg ueber den erwarteten HTTP/TS-Pfad hinaus steuern | `backend/internal/control/http/v3/handlers_intents.go:81`<br>`backend/internal/platform/net/outbound.go:79`<br>`backend/internal/infra/media/ffmpeg/plan_builder.go:205` | HTTP-Quellen werden an der API-Grenze validiert, aber beim Spawn fehlt FFmpeg-Protokollhärtung. | Minimalen `-protocol_whitelist` und ggf. Input-Format-Restriktionen fuer Netzquellen setzen. | Positivtest: erlaubte `http/https`-HLS-Quelle startet. Negativtest: Manifest mit `file:`/`concat:`/lokalem Redirect wird von FFmpeg abgelehnt. |

## Phase 4: Code Quality & Testing

| ID | Severity | File:Line | Finding | Fix |
| --- | --- | --- | --- | --- |
| QA-01 | HIGH | `backend/internal/control/http/v3/auth_strict_test.go:688` | `go test -coverprofile=coverage.out ./...` ist nicht gruen. Die Suite faellt reproduzierbar in `TestRaceSafety_ParallelIntents/Phase2_Worker_Dedup` mit Statusfolge `202,503`. | Idempotenz-/Replay-Pfad auf eine einzige Implementierung reduzieren und den Phase-2-Kontrakt wiederherstellen. |
| QA-02 | LOW | `backend/internal/control/http/v3/handlers_client_playbackinfo.go:15` | `PostItemsPlaybackInfo` ignoriert JSON-Decode-Fehler und verarbeitet einen Zero-Value-Request weiter. | Bei Decode-Fehler oder Trailing-Garbage `400 Bad Request` liefern. |
| QA-03 | LOW | `backend/internal/app/bootstrap/bootstrap.go:65` | Kritische Runtime-Pakete haben sehr geringe Coverage: `internal/app/bootstrap` 2.1%, `internal/control/http/v3/recordings/artifacts` 6.2%, `internal/domain/session/lifecycle` 10.9%, `internal/library` 10.9%, `internal/pipeline/scan` 19.8%. | Startup-/Reload-, Artifact-Resolver-, Session-Lifecycle- und Scan-Manager-Tests priorisieren. |

### Lowest Coverage Packages

| Rank | Package | Coverage |
| --- | --- | --- |
| 1 | `github.com/ManuGH/xg2g/internal/api/testutil` | 0.0% |
| 2 | `github.com/ManuGH/xg2g/internal/channels` | 0.0% |
| 3 | `github.com/ManuGH/xg2g/internal/control/clientplayback` | 0.0% |
| 4 | `github.com/ManuGH/xg2g/internal/control/http/problem` | 0.0% |
| 5 | `github.com/ManuGH/xg2g/internal/control/http/v3/types` | 0.0% |
| 6 | `github.com/ManuGH/xg2g/internal/control/recordings/capabilities` | 0.0% |
| 7 | `github.com/ManuGH/xg2g/internal/domain/session/manager/testkit` | 0.0% |
| 8 | `github.com/ManuGH/xg2g/internal/domain/session/ports` | 0.0% |
| 9 | `github.com/ManuGH/xg2g/internal/domain/vod` | 0.0% |
| 10 | `github.com/ManuGH/xg2g/internal/fsutil` | 0.0% |
| 11 | `github.com/ManuGH/xg2g/internal/infra/bus` | 0.0% |
| 12 | `github.com/ManuGH/xg2g/internal/infra/media/stub` | 0.0% |
| 13 | `github.com/ManuGH/xg2g/internal/infra/platform` | 0.0% |
| 14 | `github.com/ManuGH/xg2g/internal/netutil` | 0.0% |
| 15 | `github.com/ManuGH/xg2g/internal/normalize` | 0.0% |
| 16 | `github.com/ManuGH/xg2g/internal/pipeline/lease` | 0.0% |
| 17 | `github.com/ManuGH/xg2g/internal/streamprofile` | 0.0% |
| 18 | `github.com/ManuGH/xg2g/internal/testutil` | 0.0% |
| 19 | `github.com/ManuGH/xg2g/internal/app/bootstrap` | 2.1% |
| 20 | `github.com/ManuGH/xg2g/internal/control/http/v3/recordings/artifacts` | 6.2% |

### Command Status

| Command | Status | Evidence |
| --- | --- | --- |
| `go test -coverprofile=coverage.out ./...` | FAIL | `internal/control/http/v3` scheitert; Gesamt-Coverage aus `go tool cover` = 54.2% |
| `go tool cover -func=coverage.out \| tail -1` | OK | `total: 54.2%` |
| `govulncheck ./...` | FAIL | 15 erreichbare Go-Stdlib-Vulns auf dem im Audit benutzten `go1.25.0`; zusaetzlich 2 importierte und 3 nur-required, aber nicht erreichte Vulns |
| `golangci-lint run ./...` | OK | `0 issues.` |
| `go vet ./...` | OK | keine Ausgabe |
| `gocyclo -over 15 ./internal/` | NOT AVAILABLE | `install: go install github.com/fzipp/gocyclo/cmd/gocyclo@latest` |

## Known Issue Status

| Known ID | Status | Evidence | Note |
| --- | --- | --- | --- |
| CON-03 | Fixed on current tree | `backend/internal/pipeline/bus/memory_bus.go:53` | Kein `default:`-Drop-Pfad mehr; Publish blockiert bis Send oder `ctx.Done()`. |
| ERR-03 | Partially mitigated | `backend/internal/app/bootstrap/bootstrap.go:103` | Fehler wird jetzt geloggt, danach aber weiterhin auf Memory-Store downgraded. Nicht mehr lautlos, aber weiterhin degradierend. |
| CON-01 / CON-02 | Fixed on current tree | `backend/internal/control/vod/manager.go:75`<br>`backend/internal/control/vod/prober.go:63` | VOD-Prober und Build-Monitore haben `WaitGroup`-Join im Shutdown. |
| CFG-01 | Fixed in direct form | `backend/internal/config/env.go:23` | Direkte `os.Getenv`/`os.LookupEnv`-Reads ausserhalb `internal/config` sind weg; neues Escape-Hatch via `config.Parse*` bleibt offen und ist als `CFG-02` neu aufgenommen. |
| DG-02 | Still open / worse | `go list` audit result | `internal/control/http/v3` liegt aktuell bei 80 Imports; God-Package nicht aufgeloest. |
| ERR-01 | Still open | `backend/internal/openwebif/timer_errors.go:11` | Fehlerklassifikation basiert weiter auf lokalisierten Substrings wie `conflict`, `konflikt`, `not found`. |
| CFG-04 | Fixed on current tree | `backend/internal/control/http/v3/server.go:355` | Library-Store wird im v3-Shutdown geschlossen. |
| ERR-05 | Partially fixed | `backend/internal/control/authz/policy.go:80` | `MustScopes` panikt nicht mehr, liefert aber fuer unbekannte Operationen weiterhin `[]string{}`; das wird aktuell durch Router-Policy-Pruefung abgefangen. |

## Phase 5: Zusammenfassung

### 1. TOP-10 CRITICAL/HIGH

| Rang | ID | Severity | Datei:Zeile | One-Liner | Fix-Aufwand |
| --- | --- | --- | --- | --- | --- |
| 1 | SEC-01 | HIGH | `backend/internal/config/registry.go:130` | Default-Deployment exponiert Auth ueber Klartext-HTTP auf `:8088`. | M |
| 2 | SEC-02 | HIGH | `.env:4` | Repo enthaelt direkt verwendbares Admin-Token und reale Upstream-Credentials. | S |
| 3 | SUP-01 | HIGH | `backend/go.mod:5` | Gepinnte Toolchain liegt hinter Security-Fixes; lokales Audit lief sogar mit `go1.25.0`. | S |
| 4 | ARC-01 | HIGH | `backend/internal/control/http/v3/handlers_intents.go:205` | Doppelte Intent-Start-Implementierung driftet und bricht den Race-/Idempotenz-Kontrakt. | M |
| 5 | SUP-02 | HIGH | `.github/workflows/release.yml:28` | Release-Job zieht fremdes Binary ohne Verifikation und nutzt mutable Action-Tags. | S |

### 2. MEDIUM

| Rang | ID | Severity | Datei:Zeile | One-Liner | Fix-Aufwand |
| --- | --- | --- | --- | --- | --- |
| 1 | CFG-02 | MEDIUM | `backend/internal/infra/media/ffmpeg/adapter.go:106` | Runtime-Code liest weiterhin live ENV und umgeht die Config-Snapshot-Grenze. | M |
| 2 | SEC-04 / PIPE-01 | MEDIUM | `backend/internal/infra/media/ffmpeg/plan_builder.go:205` | FFmpeg-Netzquellen laufen ohne `-protocol_whitelist`. | S |
| 3 | SEC-03 | MEDIUM | `.gitleaks.toml:4` | Secret-Scanning verhindert den Commit realer Secrets offensichtlich nicht. | S |
| 4 | SUP-03 | MEDIUM | `.github/workflows/trivy.yml:76` | Workflow-Hardening ist inkonsistent: fehlende `permissions` und deaktivierte Enforcement-Gates. | S |

### 3. LOW / NICE-TO-HAVE

| Rang | ID | Datei:Zeile | One-Liner |
| --- | --- | --- | --- |
| 1 | QA-02 | `backend/internal/control/http/v3/handlers_client_playbackinfo.go:15` | Invalides JSON wird stillschweigend als leerer Request akzeptiert. |
| 2 | CON-04 | `backend/internal/pipeline/scan/manager.go:203` | Scan-Lifecycle faellt auf detached `context.TODO()` zurueck statt fail-closed zu sein. |
| 3 | QA-03 | `backend/internal/app/bootstrap/bootstrap.go:65` | Mehrere sicherheits- und startup-nahe Pakete sind kaum oder gar nicht abgedeckt. |

### 4. SPRINT-PLAN

- Woche 1: Security-Hardening. `SEC-01`, `SEC-02`, `SEC-03`, `SUP-01`.
- Woche 2: Stability/Concurrency. `ARC-01`, `CON-04`, bekannter Status von `DG-02` und `ERR-01` erneut verifizieren.
- Woche 3: Config/Determinism Cleanup. `CFG-02`, alte Guardrail-Luecken in `lint.yml`, Secret- und Toolchain-Enforcement.
- Woche 4: Structure + Coverage. v3-Startpfad entdoppeln, Coverage fuer Bootstrap/Artifacts/Scan/Lifecycle anheben.

### 5. VALIDIERUNGS-KOMMANDOS

```bash
cd backend
go test -race ./...
govulncheck ./...
golangci-lint run ./...
grep -rn "context.Background()" internal/ --include="*.go" | grep -v _test.go | wc -l
grep -rn "os.Getenv" internal/ cmd/ --include="*.go" | grep -v _test.go | wc -l
grep -rn '"broken pipe"\|"connection reset"\|"Konflikt"' internal/ --include="*.go" | wc -l
```
