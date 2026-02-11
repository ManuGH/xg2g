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
| XG2G_STREAM_PORT | XG2G_E2_STREAM_PORT (or `enigma2.streamPort`) | v3.5.0 |
