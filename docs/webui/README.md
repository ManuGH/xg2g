# WebUI Documentation Index

Use this area for frontend/backend UI contracts, runtime telemetry, and user
error semantics.

## Contracts

| Need | Document |
| :--- | :--- |
| How UI consumes API fields | [UI Consumption Spec](SPEC_UI_CONSUMPTION.md) |
| User-facing error mapping | [Error Map](ERROR_MAP.md) |
| Runtime telemetry fields | [Telemetry Index](TELEMETRY_INDEX.md) |

## Source Anchors

| Area | Path |
| :--- | :--- |
| WebUI app source | [apps/webui/src](../../apps/webui/src) |
| Player feature | [apps/webui/src/features/player](../../apps/webui/src/features/player) |
| WebUI README | [apps/webui/README.md](../../apps/webui/README.md) |
| Design gate | [apps/webui/DESIGN.md](../../apps/webui/DESIGN.md) |

## Maintenance Rule

When API response shape, player state semantics, or telemetry names change,
update this section and the matching frontend contract tests in the same PR.
