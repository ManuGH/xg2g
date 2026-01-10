# Startup Guide (nur für Entwickler / KI)

## Entscheidungsregel: Wann welcher Modus?

| Kontext                  | Methode              | Grund                          |
|--------------------------|----------------------|--------------------------------|
| Feature entwickeln       | ❌ NICHT systemd     | zu schwergewichtig            |
| Debugging                | ❌ NICHT systemd     | Logs & direkte Kontrolle      |
| Testen im LXC            | ❌ NICHT systemd     | sonst bremst Entwicklung      |
| **Release-Check**        | ✅ systemd           | realistische Ops-Umgebung     |
| **Homelab Enduser**      | ✅ systemd           | stabil & fail-closed          |

---

## Development / Test (JETZT)

**Kein systemd. Kein Service. Kein "System".**

### Ziel
- schnelles Iterieren
- Logs sofort sehen
- Fehler sofort reproduzieren
- keine Nebenwirkungen auf Host / SSH / System

### Option A – Dev-Loop (klassisch)

```bash
./run_dev.sh
```

**Eigenschaften:**
- Auto-Restart bei Code-Änderungen
- schnelle Rebuilds
- nicht auditfähig
- nicht release-relevant

### Option B – Docker Compose direkt (ohne systemd)

```bash
docker compose up
# oder mit rebuild
docker compose up --build
```

**Eigenschaften:**
- realistische Runtime
- gleiche Image-Basis wie System
- aber bewusst ohne Supervisor
- ideal für Feature-Tests

---

## System / Verifikation / Release (später)

**Hier MUSS systemd verwendet werden.**

### Ziel
- Fail-closed Verhalten
- kein Crash-Loop
- saubere Start/Stop-Semantik
- echte Ops-Realität

### Ablauf

```bash
systemctl start xg2g
systemctl stop xg2g
systemctl reload xg2g
```

**Nur hier gelten:**
- `ExecStartPre` Config-Checks
- harte Validierung
- canonical Ops-Path
- Audit-Gates

---

## Mentales Modell

```
systemd ist kein Dev-Tool.
systemd ist die Wahrheit des Systems.
```

**Dev ≠ System**
- schnell ≠ stabil
- iterieren ≠ absichern

---

## Checkliste: Wann ist ein Build "system-würdig"?

Ein Build sollte NUR über systemd getestet werden, wenn:

- [ ] Feature-Entwicklung abgeschlossen
- [ ] Unit-Tests laufen durch
- [ ] Integration-Tests erfolgreich
- [ ] Config-Validierung implementiert
- [ ] Fail-Closed Verhalten geprüft
- [ ] Release-Notes vorbereitet
- [ ] Breaking Changes dokumentiert

**Erst dann:** `systemctl start xg2g`

---

## Aktueller empfohlener Workflow

### Tägliche Entwicklung

```bash
# Backend + Frontend dev
./run_dev.sh

# oder nur Backend
docker compose up

# oder nur Backend mit rebuild
docker compose up --build
```

### Vor Release / System-Test

```bash
# 1. Dev-Prozesse stoppen
pkill -f run_dev.sh
pkill -f "make dev"
pkill -f xg2g

# 2. System-Test starten
systemctl start xg2g

# 3. Verifikation
systemctl status xg2g
journalctl -u xg2g -f
```

---

## Zusammenfassung

| Phase              | Start mit              | Logs mit                    |
|--------------------|------------------------|-----------------------------|
| Development        | `./run_dev.sh`         | direkt in Terminal          |
| Feature-Test       | `docker compose up`    | direkt in Terminal          |
| **Release-Check**  | `systemctl start xg2g` | `journalctl -u xg2g -f`     |
| **Production**     | `systemctl start xg2g` | `journalctl -u xg2g`        |

---

## Wichtige Hinweise

⚠️ **NICHT mischen:**
- Wenn systemd läuft, NICHT parallel `run_dev.sh` starten
- Wenn `docker compose up` läuft, NICHT parallel systemd starten
- Immer erst stoppen, dann neu starten

⚠️ **Port-Konflikte vermeiden:**
```bash
# Prüfen, was läuft
ps aux | grep -E 'xg2g|run_dev|make dev' | grep -v grep

# Alles stoppen
pkill -f run_dev.sh
pkill -f "make dev"
pkill -f xg2g
docker compose down
systemctl stop xg2g
```

---

**Erstellt:** 2026-01-09
**Zweck:** Klare Trennung von Dev- und System-Modus (nur für Entwickler / KI)
