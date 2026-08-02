# Enigma2 OpenWebif, Vu+ FBC Tuner Architecture & Resource Model (Draft & Observation Model)

**Status:** Draft – Hypotheses Pending Receiver Verification  
**Target:** Enigma2 / OpenWebif / Vu+ FBC Hardware & `xg2g` Policy Engine  
**Author:** `xg2g` Core Architecture Team  

---

## Executive Summary & Governance Notice

> [!IMPORTANT]
> **This document is a DRAFT of hypotheses and observation protocols.**  
> No statement in this document may be used as a production invariant or policy decision rule until it has been explicitly validated on the target receiver hardware and assigned a classification of `VERIFIED_BY_RECEIVER` or `VERIFIED_BY_RUNTIME_TEST`.  
> An evaluation state of `UNKNOWN` MUST NEVER default to `GRANT`.

### Classification System

Every attribute, topology claim, and API contract is tagged with one of four strict evidence classifications:

- `VERIFIED_BY_RECEIVER`: Confirmed directly by raw output from the Vu+ receiver `/proc`, `/sys`, or official API responses.
- `VERIFIED_BY_RUNTIME_TEST`: Confirmed through a controlled empirical reception test on the target SAT hardware.
- `CONFIGURED_BUT_UNVERIFIED`: Extracted from Enigma2 configuration files or APIs, but not yet proven to be physically connected or functional.
- `UNKNOWN`: Unconfirmed hypothesis. **MUST NOT be used to permit resources or grant preemption.**

---

## Part A: Verified Facts (Empirical Receiver & Spec Baseline)

> Items in Part A are verified by official specs or receiver observation.

| Item ID | Fact Description | Evidence Source | Classification |
| :--- | :--- | :--- | :--- |
| **FACT-01** | OpenWebif `/api/about` provides receiver hardware model, image version, and tuner list string. | OpenWebif REST API Spec | `VERIFIED_BY_RECEIVER` |
| **FACT-02** | Enigma2 Service Reference (`1:0:19:283D:3FB:1:C00000:0:0:0:`) encodes Service ID, TSID, ONID, and Satellite Namespace. | Enigma2 Core Architecture | `VERIFIED_BY_RECEIVER` |
| **FACT-03** | DVB-S2 satellite signals are divided into 4 polarization/band quadrants: VL, VH, HL, HH. | DVB-S2 Physics Spec | `VERIFIED_BY_RECEIVER` |
| **FACT-04** | Unicable I is defined by EN50494; Unicable II / JESS is defined by EN50607. | CENELEC Standard | `VERIFIED_BY_RECEIVER` |

---

## Part B: Hypotheses to Verify (Pending Receiver Measurement)

> Items in Part B are UNVERIFIED HYPOTHESES. They MUST NOT be used for policy enforcement or preemption decisions until assigned `VERIFIED_BY_RECEIVER` or `VERIFIED_BY_RUNTIME_TEST`.

| Hypothesis ID | Statement | Verification Method | Current Status |
| :--- | :--- | :--- | :--- |
| **HYP-01** | Tuners A and B are physical root frontends; Tuners C–H are dynamic child demodulators. | `/proc/bus/nim_sockets` & controlled reception test | `UNKNOWN` |
| **HYP-02** | Channels on the same transponder require 0 extra demodulators and 0 extra RF inputs. | Test 2 (Same transponder stream check) | `UNKNOWN` |
| **HYP-03** | Max 2 legacy LNB cables permit max 2 distinct SAT quadrants simultaneously across all 8 demodulators. | Test 4 & Test 5 (Quadrant limits) | `UNKNOWN` |
| **HYP-04** | OpenWebif `/api/subservices` reports all active tuner/demuxer allocations atomically. | OpenWebif `/api/subservices` raw JSON dump | `UNKNOWN` |
| **HYP-05** | Unicable SCR assignment guarantees 8 completely independent tuner slots across all satellites. | Receiver tuner config + SAT installation test | `CONFIGURED_BUT_UNVERIFIED` |
| **HYP-06** | `Namespace + ONID + TSID` is universally sufficient as a global transponder identity key across all sources. | Enigma2 service database analysis | `UNKNOWN` |

---

## 2. Vu+ FBC (Full Band Capture) Architecture

FBC technology fundamentally decouples physical RF inputs from digital demodulator channels.

```
                   ┌─────────────────────────────────────────┐
                   │           Vu+ FBC Receiver              │
                   │                                         │
Physical RF In 1 ──┼──► [Physical Frontend A (Root LNB 1)] ──┼──► Quadrant Q1 (e.g. 19.2°E HH)
                   │             │         │                 │
Physical RF In 2 ──┼──► [Physical Frontend B (Root LNB 2)]   │
                   │                       │                 │
                   │        Loopthrough / Dynamic Sync       │
                   │                       │                 │
                   │       ┌───────────────┼──────────────┐  │
                   │       ▼               ▼              ▼  │
                   │  [Demod C]       [Demod D]      ... [Demod H]
                   │  (Virt Tuner)    (Virt Tuner)       (Virt Tuner)
                   └─────────────────────────────────────────┘
```

### 2.1 Physical Frontends vs. Virtual Demodulators

- **Physical Frontends (Root Tuners A & B):**
  - Connected directly to physical LNB coaxial cables (`LNB In 1` and `LNB In 2`).
  - Each physical frontend controls the LNB voltage ($13\text{V} / 18\text{V}$) and 22kHz tone to select **ONE** of 4 SAT quadrants.
- **Virtual Demodulators (Tuners C, D, E, F, G, H):**
  - 6 additional digital demodulators (total 8 demodulators: A..H).
  - Contain NO physical coaxial input jacks.
  - Dynamically attach (via internal high-speed bus) to the wideband frequency spectrum captured by Physical Frontend A or Physical Frontend B.

### 2.2 Parent / Child Topology Modes

1. **Standard Dual-Coax Mode (2 Legacy Cables):**
   - Frontend A locks Quadrant $Q_1$ (e.g. Astra 19.2°E, Horizontal High).
   - Frontend B locks Quadrant $Q_2$ (e.g. Astra 19.2°E, Vertical Low).
   - Virtual Tuners C..H can tune to **ANY** transponder in $Q_1$ (via Parent A) or $Q_2$ (via Parent B).
   - Maximum simultaneous reception: Up to **8 distinct transponders** across at most **2 distinct quadrants**.
2. **Unicable / JESS Mode (EN50594 / EN50607):**
   - Single coaxial cable carries multiple User Bands (SCR frequencies).
   - ALL 8 Demodulators (A..H) are assigned their own User Band SCR frequency.
   - All 8 Tuners operate as **100% independent physical tuners** across ANY satellite, quadrant, or transponder!

---

## 3. SAT Technology Physics (DVB-S2 / DVB-S2X)

DVB-S satellite signals are divided into 4 frequency/polarization quadrants per satellite orbital position:

$$\text{Quadrant} = (\text{OrbitalPosition}, \text{Band}, \text{Polarization})$$

### 3.1 The 4 Frequency Quadrants ($2 \times 2$ Matrix)

| Quadrant Code | Band Range | Polarization | Voltage / Tone | Example Transponders (Astra 19.2°E) |
| :--- | :--- | :--- | :--- | :--- |
| **VL** | Low (10.70 – 11.70 GHz) | Vertical ($V$) | 13V, 0kHz | 11.494 V (Das Erste SD, Arte SD) |
| **VH** | High (11.70 – 12.75 GHz) | Vertical ($V$) | 13V, 22kHz | 12.545 V (ProSieben SD, SAT.1 SD) |
| **HL** | Low (10.70 – 11.70 GHz) | Horizontal ($H$) | 18V, 0kHz | 11.362 H (ZDF HD, KiKa HD) |
| **HH** | High (11.70 – 12.75 GHz) | Horizontal ($H$) | 18V, 22kHz | 11.836 H (Das Erste HD, BR HD) |

### 3.2 Transponder Physics Parameters

A complete DVB-S2 transponder descriptor contains:
- `OrbitalPosition`: Position in tenths of degrees + direction (e.g. `192` = `19.2°E`).
- `Frequency`: Center frequency in kHz (e.g. `11836000`).
- `Polarization`: `Horizontal`, `Vertical`, `CircularLeft`, `CircularRight`.
- `Band`: `Low` ($\le 11700\text{ MHz}$), `High` ($> 11700\text{ MHz}$).
- `SymbolRate`: Symbols per second (e.g. `27500000`).
- `FEC`: Forward Error Correction (`1/2`, `2/3`, `3/4`, `5/6`, `7/8`, `8/9`, `9/10`, `Auto`).
- `Modulation`: `QPSK` (DVB-S), `8PSK` (DVB-S2), `16APSK`, `32APSK`.
- `System`: `DVB-S`, `DVB-S2`, `DVB-S2X`.

---

## 4. Resource Sharing & Compatibility Rules Matrix

When a new consumer requests a channel, `xg2g` computes compatibility against active allocations using 4 cascading hardware rules:

```
                          ┌───────────────────────────────┐
                          │ Incoming Channel Request      │
                          └──────────────┬────────────────┘
                                         │
                       Same Transponder (ONID+TSID)?
                      ├── YES ──► Rule 1: GRANT (Share Demod & Tuner, 0 Extra Resource)
                      └── NO
                                         │
                     Same Satellite Quadrant (S, B, P)?
                      ├── YES ──► Rule 2: GRANT (Allocate Free Virtual Demod C..H)
                      └── NO
                                         │
                    Available Free Physical LNB In (1..2)?
                      ├── YES ──► Rule 3: GRANT (Lock 2nd Physical LNB Frontend)
                      └── NO
                                         │
                                         ▼
                     Rule 4: CONFLICT / PREEMPTION REQUIRED
                     (3rd Quadrant requested with only 2 LNB inputs)
```

### Rule 1: Same-Transponder Sharing (Zero Cost)
- **Condition:** Channel A and Channel B share the same `(Namespace, ONID, TSID)` tuple.
- **Resource Cost:** $0$ new demodulators, $0$ new frontends.
- **Decision:** Unconditional `GRANT`. Both streams are multiplexed from the same TS (Transport Stream) demuxer.

### Rule 2: Same-Quadrant Virtual FBC Sharing (Virtual Cost)
- **Condition:** Channel A and Channel B are on different transponders, BUT share the same `(OrbitalPosition, Band, Polarization)` quadrant.
- **Resource Cost:** $1$ Virtual Demodulator (e.g., Tuner C), $0$ physical LNB inputs.
- **Decision:** `GRANT` if at least 1 Virtual Demodulator (out of 8) is unallocated.

### Rule 3: Different-Quadrant Physical LNB Sharing (Physical Cost)
- **Condition:** Channel A and Channel B are in different quadrants (e.g. HH vs. VL).
- **Resource Cost:** $1$ Physical LNB Frontend (Tuner B), $1$ Virtual Demodulator.
- **Decision:** `GRANT` if Physical Frontend B is unallocated.

### Rule 4: Quadrant Conflict / Hardware Exhaustion (Conflict)
- **Condition:** 2 legacy LNB cables are locked to Quadrants $Q_1$ (HH) and $Q_2$ (VL). Incoming request requires Quadrant $Q_3$ (HL).
- **Resource Cost:** Exceeds physical LNB capacity.
- **Decision:** `PREEMPTION_REQUIRED` or `REJECT`. The request CANNOT be fulfilled without preempting an existing quadrant lock.

---

## 5. Neutral `xg2g` Domain Resource Model

`xg2g` abstracts hardware details into a vendor-neutral domain model, decoupling OpenWebif / Enigma2 from the core Policy Engine.

```
Receiver (Host Device)
 ├─ PhysicalFrontends (LNB Inputs)
 │   └─ QuadrantLock (Orbit, Band, Polarization)
 ├─ Demodulators (Tuner Slots tuner:0 .. tuner:N-1)
 │   ├─ ParentFrontendID (Root LNB)
 │   ├─ LockedTransponder (ONID, TSID, Freq)
 │   └─ State (Free, Allocated, Releasing)
 ├─ Demuxers (Transport Stream Demux Engines)
 ├─ Streams (Active Media Pipelines)
 └─ Capabilities (MaxFrontends, MaxDemods, UnicableSupported)
```

### 5.1 Go Domain Model (`internal/domain/resource`)

```go
package resource

import "time"

// SatelliteQuadrant represents the physical 2x2 LNB polarization & frequency band.
type SatelliteQuadrant struct {
	OrbitalPosition int    // e.g. 192 for 19.2°E
	Band            string // "LOW", "HIGH"
	Polarization    string // "HORIZONTAL", "VERTICAL"
}

// TransponderID uniquely identifies a DVB transport stream.
type TransponderID struct {
	Namespace int
	ONID      int
	TSID      int
	Frequency int // kHz
}

// PhysicalFrontend represents a coaxial LNB input jack.
type PhysicalFrontend struct {
	FrontendID       string // e.g. "frontend:0" (LNB In 1)
	CurrentLock      *SatelliteQuadrant
	IsLocked         bool
	UnicableUserBand int // 0 if legacy, 1..32 if SCR
}

// DemodulatorSlot represents a digital tuner slot (tuner:0 .. tuner:7).
type DemodulatorSlot struct {
	Scope              string // e.g. "tuner:0"
	ParentFrontendID   string // e.g. "frontend:0"
	CurrentTransponder *TransponderID
	IsAvailable        bool
	IsCompatible       bool
}

// ReceiverModel captures full receiver hardware capacity.
type ReceiverModel struct {
	ReceiverID       string
	ModelName        string // e.g. "Vu+ Uno 4K SE"
	Frontends        []PhysicalFrontend
	Demodulators     []DemodulatorSlot
	MaxDemuxers      int
	SupportsUnicable bool
}
```

---

## 6. Typed Policy Engine Snapshot Schema

The `SnapshotBuilder` (`internal/pipeline/policy/snapshot_builder.go`) converts OpenWebif data into this canonical `ResourceSnapshot` schema for evaluation.

### 6.1 Go Struct Schema

```go
package policy

import "time"

// ResourceCandidate Schema for Policy Engine
type ResourceCandidate struct {
	Scope        string `json:"scope"`        // "tuner:0", "tuner:1", etc.
	Compatible   bool   `json:"compatible"`   // True if tuner matches frequency/modulation
	Available    bool   `json:"available"`    // True ONLY if zero active/releasing allocations exist
	ParentScope  string `json:"parent_scope"` // "frontend:0" (LNB In 1)
	QuadrantCode string `json:"quadrant"`     // "192_HIGH_H"
}

// AllocationMetadata Schema for Policy Engine
type AllocationMetadata struct {
	AllocationID string       `json:"allocation_id"` // Unique Lease Key
	Consumer     ConsumerType `json:"consumer"`      // "LIVE_TV", "SCHEDULED_RECORDING", etc.
	Owner        string       `json:"owner"`         // Session ID or Timer ID
	Scope        string       `json:"scope"`         // "tuner:0"
	AcquiredAt   time.Time    `json:"acquired_at"`
	Sacrosanct   bool         `json:"sacrosanct"`    // True for Scheduled Recordings
	Releasing    bool         `json:"releasing"`     // True if in releasing teardown phase
	Transponder  string       `json:"transponder"`   // "C00000:1:3FB"
}

// Typed Snapshot Schema
type HardwareResourceSnapshot struct {
	Kind             ResourceKind         `json:"kind"`              // "TUNER"
	Capacity         int                  `json:"capacity"`          // 8 (Demodulator count)
	SnapshotRevision string               `json:"snapshot_revision"` // SHA-256 fingerprint
	ObservedAt       time.Time            `json:"observed_at"`
	Candidates       []ResourceCandidate  `json:"candidates"`
	Active           []AllocationMetadata `json:"active"`
}
```

### 6.2 Example Snapshot JSON (FBC Tuner Busy Event)

```json
{
  "kind": "TUNER",
  "capacity": 2,
  "snapshot_revision": "a7f39b21c4e8d012",
  "observed_at": "2026-08-02T23:30:00Z",
  "candidates": [
    {
      "scope": "tuner:0",
      "compatible": true,
      "available": false,
      "parent_scope": "frontend:0",
      "quadrant": "192_HIGH_H"
    },
    {
      "scope": "tuner:1",
      "compatible": true,
      "available": false,
      "parent_scope": "frontend:1",
      "quadrant": "192_LOW_V"
    }
  ],
  "active": [
    {
      "allocation_id": "tuner:0",
      "consumer": "LIVE_TV",
      "owner": "sess-live-101",
      "scope": "tuner:0",
      "acquired_at": "2026-08-02T23:15:00Z",
      "sacrosanct": false,
      "releasing": false,
      "transponder": "C00000:1:3FB"
    },
    {
      "allocation_id": "tuner:1",
      "consumer": "CHANNEL_SCAN",
      "owner": "scan-epg-job",
      "scope": "tuner:1",
      "acquired_at": "2026-08-02T23:25:00Z",
      "sacrosanct": false,
      "releasing": false,
      "transponder": "C00000:1:41B"
    }
  ]
}
```

---

## Part C: Controlled Empirical Receiver Observation Matrix

To convert hypotheses in Part B to verified facts in Part A, the following non-destructive, read-only diagnostic matrix will be executed on the target Vu+ receiver:

### Test Protocols (Test 0 to Test 6)

| Test ID | Test Scenario | Controlled Action | Observed Artifacts & Output | Verification Goal |
| :--- | :--- | :--- | :--- | :--- |
| **TEST-0** | Idle Baseline | Zero streams, zero timers, zero PiP. | `/api/statusinfo`, `/api/subservices`, `/proc/bus/nim_sockets` | Establish baseline idle tuner state. |
| **TEST-1** | Single Service | Tune 1 channel (e.g. Das Erste HD). | Raw JSON dumps of OpenWebif endpoints & `/sys/class/dvb` | Identify which frontend/demod handles initial stream. |
| **TEST-2** | Same Transponder | Tune 2nd channel on exact same transponder (e.g. Arte HD). | Frontend lock status, Demux PIDs, OpenWebif `/api/subservices` | Confirm if 2nd demod is allocated or 1st demod is shared. |
| **TEST-3** | Same Quadrant | Tune 2nd channel on different transponder, same quadrant (e.g. ZDF HD). | `/proc/bus/nim_sockets`, `/proc/stb/frontend` | Determine if Virtual Demod C is allocated under Parent A. |
| **TEST-4** | Different Quadrant | Tune 2nd channel on different quadrant (e.g. Vertical Low). | Frontend B status, voltage/tone state | Confirm allocation of 2nd physical LNB RF input. |
| **TEST-5** | Third Quadrant | Attempt 3rd channel requiring 3rd quadrant (e.g. Horizontal Low). | Enigma2 error response / zap rejection | Document exact failure behavior when legacy inputs exhaust. |
| **TEST-6** | Scheduled Recording | Run active timer recording while opening Live TV stream. | `/api/timerlist`, `/api/subservices`, tuner status | Document Enigma2 native timer priority & lock flags. |

---

## Conclusion & Next Steps

This document serves strictly as an **Observation Model and Hypothesis Catalog**.

- **Phase E Step E2 (Current):** Policy Engine operates in `audit-only` mode against existing tuner leases with zero mutations (`AUDIT_ONLY`).
- **Data Collection Phase:** Execute read-only diagnostic collection on target Vu+ receiver to fill Part A (`VERIFIED_BY_RECEIVER` / `VERIFIED_BY_RUNTIME_TEST`).
- **Phase E Step E3 (Future):** Hardware-aware preemption rules will ONLY be enabled after Part B hypotheses are 100% resolved and verified.

