# RED SPEC BREAK 001

**Severity:** CRITICAL
**Category:** Incompleteness
**Spec Version:** ADR-009 (2026-01-23)
**Discovered By:** Team Red
**Status:** OPEN

---

## 1. Kurzfassung (Executive Summary)

Die Spec definiert Codec-Kompatibilität rein als String-Match (`h264` vs `h264`). In der Realität ist `h264` unvollständig ohne **Profile** (Baseline, Main, High) und **Level** (3.1, 4.0, 5.2). Dieser Blind Spot führt dazu, dass die Engine `DirectPlay` genehmigt, obwohl der Client das Video (z. B. High Profile) nicht dekodieren kann, was zu **Black Screen / Client Failure** führt.

---

## 2. Betroffene Spec-Stellen

**ADR-009 Abschnitt(e):**

- §1. Inputs (Fail-Closed)

**Zitat(e):**
> Field: `Source.VideoCodec` | Type: `string` | Constraint: Normalized (lowercase)
> Field: `Source.AudioCodec` | Type: `string` | Constraint: Normalized (lowercase)

---

## 3. Angriffspunkt / Bruch

### Beschreibung

Die Spec abstrahiert sämtliche Codec-Komplexität auf einen Identifier (`h264`). Ein Vergleich `if source.VideoCodec == cap.VideoCodec` ist mathematisch korrekt, aber physikalisch falsch.
Ein Client, der nur `h264` (implizit Baseline) kann, stürzt bei `h264` (High Profile Input) ab.

### Warum das ein Problem ist

- [x] Operator-/Client-Realität nicht abbildbar
- [ ] Implizite Annahme nicht dokumentiert
- [x] Proof-System bestätigt falsche Wahrheit (Mode `DirectPlay` ist formal korrekt, real aber kaputt)

---

## 4. Konkretes Gegenbeispiel (Spec-Level)

### Hypothetischer Input

```text
Source:
  VideoCodec: "h264" (Realität: High Profile Level 5.1, 4K)
  BitrateKbps: 20000

Capabilities:
  VideoCodecs: ["h264"] (Realität: TV Hardware Decoder, Limit: Main Profile Level 4.0, 1080p)

Erwartung laut Spec
 • Regel 2 (Video): Source.VideoCodec ("h264") in Capabilities.VideoCodecs ("h264") -> MATCH.
 • Ergebnis: DirectPlay (oder DirectStream).

➡️ Widerspruch / Unklarheit:
Die Engine sendet einen Stream, den die Hardware nicht dekodieren kann.
Fail-Closed wird verletzt, weil das System "Open" fehlschlägt (Sendet Daten, die nicht funktionieren).
```

---

## 5. Aktuelles Engine-/Model-Verhalten

- **Vermuteter Mode:** `DirectPlay` (da Strings matchen)
- **Vermutete Reasons:** `[directplay_match]`

⚠️ Das System behauptet "Kompatibel", der User sieht Schwarzbild.

---

## 6. Risikoabschätzung

- **Operator verliert Vertrauen**: Der Beweis "Engine funktioniert" ist wertlos, wenn der Fernseher dunkel bleibt.
- **Client-Crash**: Manche Clients stürzen bei falschem Profil hard ab.

---

## 7. Erzwingende Konsequenz

- **Zusätzliches Input-Feld nötig**: `Codecs` müssen Objekte sein (`{id: "h264", profile: "high", level: "5.1"}`) oder RFC-6381 Strings (`avc1.640028`) unterstützen.
- **ADR-009 muss präzisiert werden**: String-Matching allein ist unzureichend für Video.

---

## 8. Red-Team-Fazit

Die Spec simplifiziert die Realität zu Tode. "H.264 ist H.264" ist eine Lüge, die jeder Video-Ingenieur sofort entlarvt. Ohne Profile/Level-Check ist die Engine für High-End-Video unbrauchbar.
