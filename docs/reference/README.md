# Reference (Diátaxis: Information-Oriented)

Reference guides provide authoritative, exact technical descriptions of the **xg2g** machinery, contracts, and configuration surfaces.

---

## What is a Reference Guide?

According to the [Diátaxis framework](https://diataxis.fr/reference/), reference material is:
- **Information-oriented**: Purely descriptive and accurate; mirrors the underlying codebase.
- **Structured for fast lookup**: Designed to be consulted while developing or debugging.
- **Neutral & complete**: States the facts, schemas, reason codes, and constraints without narrative prose.

---

## Available Reference Documentation

### Configuration & Schemas

| Reference | Scope | Format |
| :--- | :--- | :--- |
| [**Configuration Reference**](../guides/CONFIGURATION.md) | Exhaustive list of `XG2G_*` configuration variables, types, and defaults | Markdown |
| [**Config Surfaces Inventory**](../guides/CONFIG_SURFACES.md) | Generated inventory mapping every code reference to configuration keys | Markdown |
| [**Configuration Schema**](../guides/config.schema.json) | Machine-readable JSON Schema for runtime configuration validation | JSON Schema |

### APIs & Contracts

| Reference | Scope | Format |
| :--- | :--- | :--- |
| [**OpenAPI 3.0 Specification**](../../backend/api/openapi.yaml) | Canonical REST & streaming endpoints (bouquets, channels, EPG, streams, leases) | OpenAPI YAML |
| [**Lease Reason Code Matrix**](../ADR/lease_reason_matrix.md) | Formally audited catalog of all tuner lease reason codes and preemption states | Markdown |
| [**Client Profiles Catalog**](../ops/CLIENT_PROFILES.md) | Client decoding capability matrix across WebUI, iOS, Android, and Android TV | Markdown |

---

## Where to Go Next

- Want to learn the basics? Try a [**Tutorial**](../tutorials/README.md).
- Need to resolve an operational error? Check the [**How-To Guides**](../how-to/README.md).
- Looking to understand the architecture? Read the [**Explanation**](../explanation/README.md).
