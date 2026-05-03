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
| WebUI app source | [frontend/webui/src](../../frontend/webui/src) |
| Player feature | [frontend/webui/src/features/player](../../frontend/webui/src/features/player) |
| WebUI README | [frontend/webui/README.md](../../frontend/webui/README.md) |
| Design gate | [frontend/webui/DESIGN.md](../../frontend/webui/DESIGN.md) |

## Maintenance Rule

When API response shape, player state semantics, or telemetry names change,
update this section and the matching frontend contract tests in the same PR.
