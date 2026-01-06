# ADR-004: Enigma2 Trust Boundary & Timing Model

**Status**: Accepted
**Date**: 2026-01-06
**Component**: Worker / Pipeline Ingest
**Context**: Establishing a Zero-Trust relationship with Enigma2 sources to ensure Safari/AVPlayer stability.

## 1. Context & Problem Statement

Enigma2 is a broadcast-oriented Linux middleware. Its HTTP streaming output is often a side-effect, not a first-class streaming API.
Current issues include:

- **Jumping PTS/DTS/PCR**: Causes `MediaError` in strict players (Safari).
- **Inconsistent GOPs**: Breaks HLS segment alignment.
- **Unreliable Metadata**: PMT/PAT changes during stream can cause silent decoder failures.
- **Ambiguous Endings**: HTTP connection closes do not always represent intent-based session ends.

## 2. Decision

We will treat Enigma2 as an **unreliable signal provider**, not a partner. xg2g will implement a "Zero Trust" boundary where all source signals are normalized or discarded in favor of xg2g-generated timing.

## 3. Trust Matrix (v1.1)

| Signal / Metadatum | Vertrauensstatus | Verbindliche Aktion durch xg2g |
| :--- | :--- | :--- |
| **Video / Audio ES** | ⚠️ Instabil | Übernahme nur als Rohdaten. Kein Vertrauen in Kontinuität. |
| **PTS / DTS** | ❌ Aktiv Falsch | Vollständig ignorieren/überschreiben. xg2g erzeugt eigene Zeitbasis (`genpts`). |
| **PCR** | ❌ Aktiv Falsch | Vollständig ignorieren. xg2g erzeugt eigenen Wallclock-Bezug. |
| **GOP-Struktur** | ❌ Unzuverlässig | Neu erzwingen (IDR-Boundaries durch xg2g/FFmpeg). |
| **PMT / PAT** | ⚠️ Wechselhaft | Nur als Trigger für Stream-Änderungen (Codec, Auflösung). |
| **Resolution Switch** | ⚠️ Gefährlich | Hard Reset der Pipeline (neue HLS-Rendition). |
| **Stream-Ende** | ❌ Unzuverlässig | Ausschließlich durch xg2g Lease / Session-Management definiert. |

## 4. Architectural Rules

### 4.1 Timing

- **Primary Clock**: xg2g monotonic wallclock.
- **FFmpeg Mode**: No "copy timing". All output timestamps must be relative to the xg2g ingest start.
- **Drift Compensation**: Aggressive normalization to prevent AV de-sync caused by source clock drift.

### 4.2 Error & Boundary Handling

- **Hard Cuts**: pmts/resolution changes MUST trigger a pipeline restart to ensure a clean HLS rendition.
- **Isolation**: Source errors must not propagate to the HLS delivery layer. Use a "Circuit Breaker" on ingest.

### 4.3 Observability

Discarded or repaired signals must be tracked:

- `enigma_pts_jump_total`
- `enigma_pmt_change_total`
- `enigma_stream_reset_total`

## 5. Consequences

- **Stability**: Much higher compatibility with Safari and AVPlayer.
- **Resource Usage**: Slightly higher CPU for GOP-enforcement/Transcoding where "copy" was previously attempted.
- **Complexity**: Ingest logic becomes a "repair shop" rather than a passive proxy.
