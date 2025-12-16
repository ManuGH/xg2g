---
name: "Coverage Improvement"
about: "Gezielte Testabdeckung für eine Komponente erhöhen"
labels: ["coverage-improvement", "testing"]
---

## Komponente

- [ ] api
- [ ] proxy
- [ ] daemon
- [ ] epg
- [ ] playlist
- [ ] owi

## Ist-/Soll-Coverage

- **Ist:** <!-- z.B. 64.4% -->
- **Ziel (PR):** <!-- z.B. +3..8 %-Pkt. oder API ≥70% -->

## Maßnahmen (ankreuzen)

- [ ] Interfaces extrahieren / Abhängigkeiten invertieren
- [ ] Table-driven Tests für Handler
- [ ] Circuit-Breaker State-Tests
- [ ] Fake-FFmpeg / Fake-Prober
- [ ] Fehlerpfade (Timeout, Exit≠0, leere Streams)
- [ ] Contract-Test gegen Mock-OWI (CI-only)
- [ ] I/O-Abstraktion (`io.Reader`/`io.Writer`)
- [ ] Chaos-nahe Tests (Latenz, Upstream-Fehler)

## Test-Flags

- [ ] unittests
- [ ] integration
- [ ] contract

## Abnahmekriterien

- [ ] Patch ≥90%
- [ ] Komponente +X %-Pkt.
- [ ] Flaky-Rate <5%
- [ ] CI-Zeit nicht erhöht (oder begründet)
- [ ] Tests sind deterministisch (keine Sleeps)

## Referenz

- [Coverage Operations Runbook - Section 9](../../docs/COVERAGE_OPERATIONS.md#9-coverage-improvement-strategy-api--proxy)
- [Current Coverage Baseline](../../docs/COVERAGE_OPERATIONS.md#91-quick-wins-1-2-prs)

## Implementierungsplan

<!-- Beschreibe die konkreten Schritte: -->
1.
2.
3.

## Definition of Done

- [ ] Tests geschrieben und lokal erfolgreich
- [ ] Coverage-Ziel erreicht (lokal mit `go tool cover`)
- [ ] CI passing (alle Checks grün)
- [ ] Codecov Patch Coverage ≥90%
- [ ] Keine Flaky Tests (3x lokal ohne Fehler)
- [ ] Code-Review approved
