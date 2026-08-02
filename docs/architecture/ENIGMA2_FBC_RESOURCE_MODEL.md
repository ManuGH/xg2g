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

### Evidence Classification System

Every attribute, topology claim, and API contract is tagged with one of six evidence classifications:

- `VERIFIED_BY_SPEC`: Confirmed by official DVB standards (e.g., ETSI DVB-S2 specifications, CENELEC EN50494 / EN50607).
- `VERIFIED_BY_RECEIVER`: Confirmed directly by raw output from the Vu+ receiver `/proc`, `/sys`, or official API responses.
- `VERIFIED_BY_RUNTIME_TEST`: Confirmed through a controlled empirical reception test on the target SAT hardware.
- `CONFIGURED_BUT_UNVERIFIED`: Extracted from Enigma2 configuration files or APIs, but not yet proven to be physically connected or functional.
- `HYPOTHESIS`: Unconfirmed assumption. **MUST NOT be used as a production invariant.**
- `UNKNOWN`: Unconfirmed state. **MUST NOT be used to permit resources or grant preemption.**

---

## Part A: Verified Specifications (Standards & DVB Physics Baseline)

> Items in Part A are verified by official standards or DVB specifications (`VERIFIED_BY_SPEC`).

| Item ID | Fact Description | Exact Source Reference | Classification |
| :--- | :--- | :--- | :--- |
| **SPEC-01** | Enigma2 Service Reference string (`1:0:19:283D:3FB:1:C00000:0:0:0:`) encodes Service ID, TSID, ONID, and Satellite Namespace. | Enigma2 OpenWebif Source Documentation | `VERIFIED_BY_SPEC` |
| **SPEC-02** | DVB-S2 satellite signals are physically divided into 4 polarization/band quadrants: VL (Vertical Low), VH (Vertical High), HL (Horizontal Low), HH (Horizontal High). | ETSI EN 302 307 DVB-S2 Specification | `VERIFIED_BY_SPEC` |
| **SPEC-03** | Unicable I (Single Cable Distribution) is defined by CENELEC EN50494; Unicable II / JESS is defined by CENELEC EN50607. | CENELEC EN50494 / EN50607 Standards | `VERIFIED_BY_SPEC` |

---

## Part B: Hypotheses & Capability Probes to Verify (Pending Receiver Measurement)

> Items in Part B are UNVERIFIED HYPOTHESES or CAPABILITY PROBES. They **MUST NOT** be used for policy enforcement or preemption decisions until assigned `VERIFIED_BY_RECEIVER` or `VERIFIED_BY_RUNTIME_TEST`. Production Usable: **NO**.

| Hypothesis ID | Statement / Probe | Verification Method | Source Endpoint / Path | Current Status |
| :--- | :--- | :--- | :--- | :--- |
| **PROBE-01** | OpenWebif API endpoints (`/api/about`, `/api/statusinfo`, `/api/tunersignal`, `/api/timerlist`, `/api/getallservices`) exist and return HTTP 200 JSON/XML. | Passive HTTP capability probe | OpenWebif REST API | `CONFIGURED_BUT_UNVERIFIED` |
| **HYP-01** | Tuners A and B are physical root frontends; Tuners C–H are dynamic child demodulators. | Inspection & empirical lock test | `/proc/bus/nim_sockets`, `/sys/class/dvb` | `HYPOTHESIS` |
| **HYP-02** | Channels on the exact same transponder require 0 extra demodulators and 0 extra RF inputs. | Test 2 (Same transponder stream test) | OpenWebif `/api/subservices` | `HYPOTHESIS` |
| **HYP-03** | Max 2 legacy LNB cables permit max 2 distinct SAT quadrants simultaneously across all 8 demodulators. | Test 4 & Test 5 (Quadrant limits) | `/proc/stb/frontend/0/` | `HYPOTHESIS` |
| **HYP-04** | OpenWebif `/api/subservices` reports all active tuner/demuxer allocations atomically. | OpenWebif `/api/subservices` raw JSON dump | `/api/subservices` | `HYPOTHESIS` |
| **HYP-05** | Unicable SCR assignment guarantees 8 completely independent tuner slots across all satellites. | Receiver tuner config + SAT installation test | `/etc/enigma2/settings` | `CONFIGURED_BUT_UNVERIFIED` |
| **HYP-06** | `Namespace + ONID + TSID` is universally sufficient as a global transponder identity key across all sources. | Enigma2 service database analysis | `/etc/enigma2/lamedb` | `HYPOTHESIS` |

---

## Part C: FBC Hardware & Topology Hypotheses

> [!CAUTION]
> **HYPOTHESIS BOX (NOT PRODUCTION USABLE)**  
> **Evidence Status:** `HYPOTHESIS`  
> **Required Observation:** Receiver hardware inspection via `/proc/bus/nim_sockets` and OpenWebif `/api/about` output.  
> **Production Usable:** `NO`

### C.1 Hypothesized Physical vs. Virtual Tuner Topology

```
                  ┌───────────────────────────────────────────┐
                  │        Vu+ FBC Receiver (HYPOTHESIS)      │
                  │                                           │
Physical Input 1 ─┼──► [Hypothesized Root Frontend A] ────────┼──► Quadrant Q1 (e.g. 19.2°E HH)
                  │             │          │                  │
Physical Input 2 ─┼──► [Hypothesized Root Frontend B]         │
                  │                        │                  │
                  │       Hypothesized Loopthrough Bus        │
                  │                        │                  │
                  │       ┌────────────────┼───────────────┐  │
                  │       ▼                ▼               ▼  │
                  │  [Demod C]        [Demod D]       ... [Demod H]
                  │  (Virtual)        (Virtual)           (Virtual)
                  └───────────────────────────────────────────┘
```

- **Physical Frontends (Hypothesis A & B):**
  - **Hypothesis:** Tuner A and Tuner B connect directly to LNB In 1 and LNB In 2, controlling LNB voltage ($13\text{V} / 18\text{V}$) and 22kHz tone.
  - **Status:** `HYPOTHESIS` — Must be verified against actual receiver `/proc/bus/nim_sockets`.
- **Virtual Demodulators (Hypothesis C..H):**
  - **Hypothesis:** Tuners C through H are digital demodulators without physical coaxial inputs that attach dynamically to Frontend A or B.
  - **Status:** `HYPOTHESIS` — Must be verified by empirical tuner allocation tests.

### C.2 Hypothesized Unicable / JESS Mode (EN50494 / EN50607)

- **Hypothesis:** Under EN50494 (Unicable I) or EN50607 (Unicable II / JESS), each of the 8 demodulators is assigned a dedicated User Band SCR frequency, making all 8 demodulators operate independently.
- **Status:** `CONFIGURED_BUT_UNVERIFIED` — Depends on real LNB/multiswitch wiring, available SCR frequencies, and DiSEqC setup.

---

## Part D: Candidate Compatibility Hypotheses for Runtime Verification

> [!CAUTION]
> **HYPOTHESIS BOX (NO AUTOMATIC GRANT PERMITTED)**  
> The following rules compute **technical intermediate RF states**, NOT final `GRANT` or `PREEMPTION_REQUIRED` decisions. Final decisions require checking RF state + Demod Capacity + Demux Filter Capacity + CI/Decryption Capacity + Pipeline Capacity + Policy Arbitration.

```
                          ┌───────────────────────────────┐
                          │ Incoming Channel Request      │
                          └──────────────┬────────────────┘
                                         │
                       Same Transponder (ONID+TSID)?
                      ├── YES ──► State: RF_PATH_REUSE_POSSIBLE (Hypothesis H-1)
                      └── NO
                                         │
                     Same Satellite Quadrant (S, B, P)?
                      ├── YES ──► State: ADDITIONAL_DEMOD_REQUIRED (Hypothesis H-2)
                      └── NO
                                         │
                    Available Free Physical LNB In (1..2)?
                      ├── YES ──► State: ADDITIONAL_FRONTEND_REQUIRED (Hypothesis H-3)
                      └── NO
                                         │
                                         ▼
                     State: RF_CAPACITY_CONFLICT (Hypothesis H-4)
```

| Intermediate RF State Code | Condition | Resource Implications | Policy Decision |
| :--- | :--- | :--- | :--- |
| `RF_PATH_REUSE_POSSIBLE` | Request matches an active `(Namespace, ONID, TSID)` transponder. | $0$ new RF locks, $0$ new demodulators. Demux/CI/pipeline capacity MUST still be checked. | **REQUIRES_FULL_RESOURCE_CHECK** |
| `ADDITIONAL_DEMOD_REQUIRED` | Request matches an active Satellite Quadrant `(Orbit, Band, Pol)`. | $1$ additional Virtual Demodulator required. $0$ new physical RF inputs. | **REQUIRES_DEMOD_AVAILABILITY_CHECK** |
| `ADDITIONAL_FRONTEND_REQUIRED` | Request is in a new Satellite Quadrant. | $1$ additional Physical LNB input + $1$ Virtual Demodulator required. | **REQUIRES_FRONTEND_AVAILABILITY_CHECK** |
| `RF_CAPACITY_CONFLICT` | Request requires a 3rd Satellite Quadrant with only 2 physical LNB inputs. | Physical LNB capacity exhausted. | **RF_CONFLICT_TRIGGERED** |
| `RF_COMPATIBILITY_UNKNOWN` | Any hardware, frequency, or configuration parameter is missing or unverified. | Hardware capabilities cannot be determined. | **MUST_NOT_GRANT (FAIL CLOSED)** |

---

## Part E: Observed Receiver Model vs. Proposed Neutral Domain Model

### E.1 Observed Receiver Model (Raw Evidence Schema)

> This section represents raw evidence structures to be populated directly from receiver diagnostics.

```go
package evidence

import "time"

// ObservedValue wraps raw values with evidence provenance.
type ObservedValue[T any] struct {
	Value      T                  `json:"value"`
	Confidence string             `json:"confidence"` // "VERIFIED_BY_RECEIVER", "CONFIGURED_BUT_UNVERIFIED", etc.
	Source     string             `json:"source"`     // e.g. "/proc/bus/nim_sockets" or "/api/about"
	ObservedAt time.Time          `json:"observed_at"`
	RawPath    string             `json:"raw_path"`   // Path to raw JSON/txt dump
}

type RawReceiverIdentity struct {
	ModelName    ObservedValue[string]   `json:"model_name"`
	ImageVersion ObservedValue[string]   `json:"image_version"`
	WebifVersion ObservedValue[string]   `json:"webif_version"`
	NimSockets   ObservedValue[[]string] `json:"nim_sockets"`
}
```

### E.2 Proposed Neutral Domain Model (`xg2g` Target Schema)

> [!NOTE]
> **PROPOSAL NOTICE:**  
> This schema is proposed for `xg2g` internal domain modeling and is **NOT evidence** that the Enigma2 receiver or OpenWebif API exposes every field.

```go
package resource

import "time"

// SatelliteQuadrant represents physical 2x2 LNB polarization & frequency band.
type SatelliteQuadrant struct {
	OrbitalPosition int    `json:"orbital_position"` // e.g. 192 for 19.2°E
	Band            string `json:"band"`             // "LOW", "HIGH"
	Polarization    string `json:"polarization"`     // "HORIZONTAL", "VERTICAL"
}

// ServiceIdentity uniquely identifies a channel at the service layer.
type ServiceIdentity struct {
	ServiceReference  string `json:"service_reference"`
	ServiceID         uint16 `json:"service_id"`
	TransportStreamID uint16 `json:"transport_stream_id"`
	OriginalNetworkID uint16 `json:"original_network_id"`
	Namespace         uint32 `json:"namespace"`
}

// SatelliteDelivery represents physical DVB-S2 tuning parameters.
type SatelliteDelivery struct {
	OrbitalPosition int    `json:"orbital_position"`
	FrequencyKHz    uint32 `json:"frequency_khz"`
	SymbolRate      uint32 `json:"symbol_rate"`
	Polarization    string `json:"polarization"`
	Band            string `json:"band"`
	DeliverySystem  string `json:"delivery_system"`
	Modulation      string `json:"modulation"`
	FEC             string `json:"fec"`
}

// ReceiverModel captures overall receiver capabilities.
type ReceiverModel struct {
	ReceiverID       string `json:"receiver_id"`
	ModelName        string `json:"model_name"`
	SupportsUnicable bool   `json:"supports_unicable"`
}
```

---

## Part F: Empirical Receiver Observation Protocols

Observation is strictly divided into two operational phases:

### Phase 1: PASSIVE_COLLECTION (Read-Only Diagnostic Probe)
- **Tool:** `scripts/collect_receiver_diagnostics.sh`
- **Output Target:** `var/diagnostics/enigma2/<UTC_TIMESTAMP>/` (gitignored, restrictive `0700` umask)
- **Allowed Actions:** Read-only HTTP GET queries to allowlisted OpenWebif endpoints; read-only file reads of `/proc`, `/sys`, and `/etc/enigma2/`.
- **Forbidden Actions:** NO Zapping, NO Channel Switching, NO Stream Start, NO PiP, NO Timer Creation, NO Recording Start, NO Config Mutation, NO Enigma2 Restart.

### Phase 2: CONTROLLED_RUNTIME_TEST — REQUIRES EXPLICIT USER APPROVAL
- **Tool:** `scripts/run_receiver_runtime_tests.sh` (Future tool)
- **Pre-requisite:** Requires explicit user approval before execution.

| Test ID | Test Scenario | Controlled Action | Target Observation Artifacts | Verification Goal |
| :--- | :--- | :--- | :--- | :--- |
| **TEST-0** | Idle Baseline | Zero streams, zero timers, zero PiP. | `/api/statusinfo`, `/proc/bus/nim_sockets` | Establish baseline idle tuner state. |
| **TEST-1** | Single Service | Tune 1 channel (e.g. Das Erste HD). | Raw dumps of `/api/subservices`, `/sys/class/dvb` | Identify active frontend/demod. |
| **TEST-2** | Same Transponder | Tune 2nd channel on exact same transponder. | Demux PIDs, `/api/subservices` | Confirm if 2nd demod is allocated or 1st demod is shared. |
| **TEST-3** | Same Quadrant | Tune 2nd channel on different transponder, same quadrant. | `/proc/bus/nim_sockets`, `/proc/stb/frontend` | Determine if Virtual Demod C is allocated under Parent A. |
| **TEST-4** | Different Quadrant | Tune 2nd channel on different quadrant. | Frontend B status, voltage/tone state | Confirm allocation of 2nd physical LNB RF input. |
| **TEST-5** | Third Quadrant | Attempt 3rd channel requiring 3rd quadrant. | Enigma2 error response / zap rejection | Document exact failure behavior when legacy inputs exhaust. |
| **TEST-6** | Scheduled Recording | Run active timer recording while opening Live TV stream. | `/api/timerlist`, `/api/subservices` | Document Enigma2 native timer priority & lock flags. |

---

## Part G: Configuration Drift Detection & Capability Revalidation Protocol

Even after initial empirical verification, receiver physical environment or Enigma2 tuner configurations can drift (e.g. moving receiver to a new location, replacing SAT cables, changing from Legacy to Unicable/JESS, modifying SCR frequencies, or disconnecting a tuner cable).

```
                      ┌────────────────────────────────────────┐
                      │ Receiver Startup / Periodic Check      │
                      └──────────────────┬─────────────────────┘
                                         │
                   Compute ReceiverConfigFingerprint (SHA-256)
                                         │
                    Fingerprint Matches Cached Baseline?
                   ├── YES ──► Continue Current Mode (Audit-Only / Enforce)
                   └── NO
                                         │
                                         ▼
                     DRIFT DETECTED: FBC SETUP CHANGED
                     ├─ Mark Hardware Model STALE_NEED_REVALIDATION
                     ├─ Lock Automated Preemption (Force AUDIT_ONLY)
                     ├─ Trigger Phase 1 Passive Diagnostic Collector
                     └─ Demand Controlled Phase 2 Revalidation
```

### G.1 `ReceiverConfigFingerprint` Specification

At startup or periodic intervals, `xg2g` computes a canonical SHA-256 fingerprint over:
1. Receiver Hardware Model & Nim Sockets (`/proc/bus/nim_sockets`)
2. Enigma2 Tuner Settings (`/etc/enigma2/settings`)
3. Configured Unicable SCR Frequencies & User Bands
4. Configured Satellite Orbital Positions & DiSEqC Setup
5. Image Build & OpenWebif API Version (`/api/about`)

### G.2 Two-Level Drift Safeguard Protocol

1. **Level 1 — Configuration Drift Detection:**
   - Detects any change in receiver model, NIM sockets, tuner mode, SCR frequencies, or connected satellite positions.
   - Instantly revokes valid baseline status and locks preemption.
2. **Level 2 — Capability Revalidation:**
   - Detects physical wiring mismatch (e.g. 2nd cable configured in Enigma2 but physically disconnected).
   - Executes Phase 1 Passive Collection (`scripts/collect_receiver_diagnostics.sh`) followed by Phase 2 Controlled Runtime Revalidation before re-enabling automated preemption.

---

## Conclusion & Next Steps

This document serves strictly as an **Observation Model, Hypothesis Catalog, and Drift Safeguard Protocol**.

1. **Phase E Step E2 (Current):** Policy Engine operates in `audit-only` mode against existing tuner leases with zero mutations (`AUDIT_ONLY`).
2. **Phase 1 Passive Diagnostic Collector:** Execute `scripts/collect_receiver_diagnostics.sh` to gather raw evidence files into `var/diagnostics/enigma2/` (gitignored, restrictive `0700` umask).
3. **Phase 2 Runtime Testing:** Execute controlled runtime tests ONLY after receiving explicit user approval.
4. **Configuration Drift Safeguard:** Enforce `ReceiverConfigFingerprint` check before allowing future hardware-aware preemption in Phase E Step E3.


