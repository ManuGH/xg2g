# xg2g Deprecation Policy

## Purpose

This policy standardizes how features, configurations, and environment variables
are deprecated and removed.

## Cycle

1. Stage 1: Warning
2. Stage 2: Fail-Start
3. Stage 3: Removal

## Active Deprecations

| Item | Replacement | Remove In |
|:---|:---|:---|
| XG2G_STREAM_PORT | XG2G_E2_STREAM_PORT | v3.5.0 |

Legacy receiver aliases such as `XG2G_STREAM_PORT`, `XG2G_OWI_*`, and
`XG2G_USE_WEBIF_STREAMS` are not accepted by the daemon. Operators should use
the canonical `XG2G_E2_*` surface or leave deprecated stream-port overrides
unset.
