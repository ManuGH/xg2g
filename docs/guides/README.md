# User Guides

User-facing setup, configuration, and reference material for running xg2g.
Operational incident response lives in [`docs/ops/`](../ops/README.md).

## Guides

| Need | Document |
| :--- | :--- |
| Get streaming for the first time | [Getting Started](GETTING_STARTED.md) |
| Configure xg2g (essentials first) | [Configuration](CONFIGURATION.md#essential-start-here) |
| Full configuration reference | [Configuration](CONFIGURATION.md) |
| Generated option reference | [Reference](REFERENCE.md) |
| Fix a problem | [Troubleshooting](TROUBLESHOOTING.md) |

## Machine-Readable Reference

| Artifact | Purpose |
| :--- | :--- |
| [config.schema.json](config.schema.json) | JSON schema for config-aware tooling and editor validation. |
| [CONFIG_SURFACES.md](CONFIG_SURFACES.md) | Generated inventory of every file referencing `XG2G_*` keys (config-ownership reference). |

## Maintenance Rule

Guides explain the supported path for users. Historical failures, incidents, and
host-specific notes belong in [`docs/ops/`](../ops/README.md).
