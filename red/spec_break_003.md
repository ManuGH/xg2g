# RED SPEC BREAK 003

**Severity:** HIGH
**Category:** Terminology Drift
**Spec Version:** ADR-009 (2026-01-23)
**Discovered By:** Team Red
**Status:** OPEN

---

## 1. Kurzfassung (Executive Summary)

Die Spec verwendet den Begriff "Unknown" in §1 ("Unknown Semantics: Deny") ohne Definition. Es ist unklar, ob "Unknown" ein leerer Wert (`""`), ein technischer Sentinel (`"unknown"`) oder ein *unbekannter, aber valider* Wert (`"av1"` bei altem Server) ist. Diese Ambivalenz gefährdet die Vorwärtskompatibilität (Fail-Closed bei neuen Codecs statt Transcode-Fallback).

---

## 2. Betroffene Spec-Stellen

**ADR-009 Abschnitt(e):**

- §1. Inputs (Fail-Closed)

**Zitat(e):**
> Any "unknown" or zero-value in critical fields results in `ModeDeny`.
> Field: `Source.VideoCodec` | Unknown Semantics: `Deny`

---

## 3. Angriffspunkt / Bruch

### Beschreibung

"Unknown" ist nicht definiert.
Szenario: Ein neuer Codec (`av1`) kommt auf den Markt. Der Extractor erkennt ihn korrekt als `av1`.
Capabilities kennen `av1` noch nicht.
Ist `av1` "Unknown" (weil nicht im Enum)? -> Dann **Deny** (laut §1).
Oder ist `av1` "Known but Unsupported"? -> Dann **Transcode** (laut §4 Regel 2).

Spec §1 sagt pauschal "Unknown ... results in ModeDeny". Das suggeriert, dass alles, was der Engine nicht *bekannt* ist, geblockt wird – noch *bevor* Transcode geprüft wird.

### Warum das ein Problem ist

- [x] Mehrdeutige Auslegung möglich
- [x] Widerspruch zwischen §1 (Deny) und §4 (Transcode bei Mismatch)
- [x] Blockiert Einführung neuer Codecs (Server-Update zwingend nötig, Transcode Fallback greift nicht).

---

## 4. Konkretes Gegenbeispiel (Spec-Level)

### Hypothetischer Input

```text
Source:
  VideoCodec: "av1" (Valid string, aber Server kennt ihn noch nicht in Whitelist?)

Capabilities:
  VideoCodecs: ["h264"]
  
Policy:
  AllowTranscode: true

Erwartung laut Spec
 • Interpretation A (§1): "av1" ist dem System unbekannt/fremd -> "Unknown" -> DENY.
 • Interpretation B (§4): "av1" ist nicht in Caps -> Mismatch -> TRANSCODE.

➡️ Widerspruch:
Wenn §1 greift ("Unknown results in Deny"), wird Transcode fälschlicherweise blockiert.
```

---

## 5. Aktuelles Engine-/Model-Verhalten

- **Aktueller Code:** Model prüft `isUnknown(s)` via `s == "" || s == "unknown"`.
- **Implikation:** Engine behandelt *"av1"* als "Known" (valid string) und würde Transcodieren.
- **Spec-Konformität:** Unklar. Wenn Spec mit "Unknown" eigentlich "nicht in Whitelist" meinte, ist Engine zu permissiv. Wenn Spec "Extrahierung fehlgeschlagen" meinte, ist der Begriff "Unknown" extrem unglücklich gewählt (sollte "Missing" heißen).

---

## 6. Risikoabschätzung

- **Terminologie-Drift**: Entwickler interpretieren "Unknown" als "Empty", Produktmanager als "New Feature".
- **Operationelles Risiko**: Bei neuen Codecs (z.B. iPhone nimmt plötzlich in HEVC auf, Server kennt String noch nicht?) -> Wenn Logic "Unknown=Deny" greift, haben Nutzer keine Wiedergabe, obwohl FFMPEG transcodieren könnte.

---

## 7. Erzwingende Konsequenz

- **ADR-009 präzisieren**:
  - Begriff "Unknown" streichen. Ersetzen durch "Missing" oder "Unidentified" (für leere/ungültige Metadaten).
  - Klarstellen: "Unrecognized values (valid strings) must be treated as Unsupported (-> Transcode), not Unknown (-> Deny)."

---

## 8. Red-Team-Fazit

Die Spec vermischt "Datenqualität" (Metadaten fehlen) mit "Feature-Support" (Codec fremd). Das muss getrennt werden. "Garbage In -> Deny" ist ok. "New In -> Transcode" ist Pflicht. Die aktuelle Formulierung riskiert "New In -> Deny".
