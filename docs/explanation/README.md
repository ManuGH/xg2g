# Explanation (Diátaxis: Understanding-Oriented)

Explanation documents provide deep technical background, conceptual models, and architectural rationale for **xg2g**.

---

## What is an Explanation?

According to the [Diátaxis framework](https://diataxis.fr/explanation/), an explanation is:
- **Understanding-oriented**: Illuminates *why* things are designed the way they are.
- **Contextual & discursive**: Connects individual components to the larger system architecture.
- **Rooted in principles**: Explains tradeoffs, design patterns, invariant rules, and failure modes.

---

## Available Architecture & Explanation Documentation

### System Overview & Foundational Invariants

| Document | Topic | Key Concepts |
| :--- | :--- | :--- |
| [**2026 System Overview**](../arch/SYSTEM_OVERVIEW_2026.md) | High-level system architecture | Physical vs. virtual tuners, network boundaries, storage contracts |
| [**Engineering Charter**](../ENGINEERING_CHARTER.md) | Governing engineering principles | Zero-drift governance, failure domain containment, hermetic verification |
| [**Architecture Invariants (ADR-005)**](../ADR/005-Architecture-Invariants.md) | Non-negotiable structural rules | Dependency boundaries, panic elimination, deterministic behavior |
| [**Concurrency Manifest (ADR-006)**](../ADR/006-Concurrency-Manifest.md) | Thread safety & synchronization | Mutex hierarchy, goroutine lifecycle, cooperative thread pool contention |

### Tuner Lease & Resource Arbitration

| Document | Topic | Key Concepts |
| :--- | :--- | :--- |
| [**Composite Lease Model (ADR-029)**](../ADR/029-resource-arbitration-composite-lease-model.md) | Physical tuner resource arbitration | Multi-resource leases, preemption priorities, tuner sharing vs. dedication |
| [**Lease Reconciliation (ADR-030)**](../ADR/030-lease-reconciliation.md) | Startup recovery & orphaned leases | 3-phase reconciliation (Snapshot, Analysis, Remediation), StartupGate |

### Media Pipeline & Codec Handling

| Document | Topic | Key Concepts |
| :--- | :--- | :--- |
| [**Codec & Container Matrix**](../arch/CODEC_MATRIX.md) | Hardware acceleration & transcoding | VAAPI, NVENC, VideoToolbox, copy-mode remuxing, codec fallback paths |
| [**Native WebKit HEVC Copy (ADR-026)**](../ADR/026-native-webkit-hls-hevc-copy.md) | Zero-transcode Apple HLS streaming | fMP4 packaging, Safari/AVPlayer native HEVC playback without transcoding |

### Architecture Decision Records (ADRs)

| Document | Description |
| :--- | :--- |
| [**ADR Catalog**](../ADR/README.md) | Complete chronological index of all Architectural Decision Records. |

---

## Where to Go Next

- Want practical step-by-step learning? Start with [**Tutorials**](../tutorials/README.md).
- Need to resolve an issue in production? Check [**How-To Guides**](../how-to/README.md).
- Looking up specific configuration keys or API specs? Browse [**Reference**](../reference/README.md).
