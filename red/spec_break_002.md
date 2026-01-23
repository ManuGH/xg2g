# RED SPEC BREAK 002

**Severity:** HIGH
**Category:** Precedence Hole
**Spec Version:** ADR-009 (2026-01-23)
**Discovered By:** Team Red
**Status:** OPEN

---

## 1. Kurzfassung (Executive Summary)

Regel 1 ("Container Not Supported") fordert "Fail checks (May fallback to Transcode)". Dies steht im Widerspruch zu Regel 5 ("DirectStream"), die erlaubt, Container durch Remuxing zu tauschen. Die Spec definiert nicht, ob ein Container-Mismatch `DirectStream` blockiert. Die aktuelle Implementierung (Engine) ignoriert Regel 1 für DirectStream, was der Spec widerspricht ("Fail checks").

---

## 2. Betroffene Spec-Stellen

**ADR-009 Abschnitt(e):**

- §4. Decision Rules (Precedence Order)

**Zitat(e):**
>
> 1. **Rule-Container**: If `Source.Container` not in `Capabilities.Containers` -> **Fail checks** (May fallback to Transcode).
> ...
> 2. **Rule-DirectStream**: If `SupportsHLS` AND Codecs Compatible -> **DirectStream**.

---

## 3. Angriffspunkt / Bruch

### Beschreibung

"Fail checks" in Regel 1 ist mehrdeutig.

- Interpretation A: "Fail checks" = Abbruch aller Prüfungen außer Transcode. -> DirectStream verboten.
- Interpretation B: "Fail checks" = "Direct Play checks fail". -> DirectStream erlaubt (da Container irrelevant bei Remux).

Die Spec erwähnt nur "Fallback to Transcode" explizit, schweigt aber zu DirectStream.

### Warum das ein Problem ist

- [x] Mehrdeutige Auslegung möglich
- [x] Mehrere Regeln gleichzeitig anwendbar (Regel 1 Fail vs Regel 5 Match)
- [x] Regelreihenfolge suggeriert, dass Container-Priotät *über* DirectStream steht, was technisch falsch ist (Container ist für DS irrelevant).

---

## 4. Konkretes Gegenbeispiel (Spec-Level)

### Hypothetischer Input

```text
Source:
  Container: "mkv" (nicht unterstützt)
  VideoCodec: "h264" (unterstützt)
  AudioCodec: "aac" (unterstützt)

Capabilities:
  Containers: ["mp4"]
  SupportsHLS: true

Policy:
  AllowTranscode: true (oder false, das Problem existiert unabhängig)

Erwartung laut Spec
 • Regel 1: Container mismatch -> "Fail checks".
 • Darf Engine jetzt Regel 5 (DirectStream) prüfen?
 • Spec sagt: "(May fallback to Transcode)". Erwähnt DS nicht.

➡️ Widerspruch:
Wenn Engine DirectStream wählt (was effizient wäre), verletzt sie Regel 1 (Fail checks).
Wenn Engine Deny/Transcode wählt, verschwendet sie Ressourcen (Remux wäre möglich).
```

---

## 5. Aktuelles Engine-/Model-Verhalten

- **Vermuteter Mode:** `DirectStream` (Engine Check: `!CanContainer` blockiert nur `DirectPlay`, nicht `DirectStream`).
- **Aktueller Code:** `evaluateDecision` speichert `ReasonContainerNotSupported`, prüft aber weiter und returned `DirectStream` wenn HLS ok.
- **Spec-Konformität:** NEIN. Spec sagt "Fail checks", Code macht "Continue".

---

## 6. Risikoabschätzung

- **Silent Spec Drift**: Code implementiert sinnvolles Verhalten ("Repackaging"), Spec verbietet es formal ("Fail checks").
- **Determinismus-Gefahr**: Wenn ein Entwickler "Fail checks" wörtlich nimmt, bricht DirectStream für MKV-Dateien weg -> Massiver Transcoding-Anstieg (Kostenexplosion).

---

## 7. Erzwingende Konsequenz

- **Regelreihenfolge explizit korrigieren**:
  - Entweder: Regel 1 gilt nur für DirectPlay.
  - Oder: DirectStream-Regel muss *vor* Container-Check (bzw. Container-Check ist Teil von DirectPlay-Regel).
- **ADR-009 präzisieren**: "Fail checks for DirectPlay" statt pauschal "Fail checks".

---

## 8. Red-Team-Fazit

Die Spec versteht den Unterschied zwischen Container (Transport) und Stream (Payload) nicht korrekt. Ein Container-Mismatch ist für DirectStream irrelevant, wird aber in der Spec als globaler "Fail" behandelt. Das muss repariert werden, sonst ist die Spec blockierend für effizientes Streaming.
