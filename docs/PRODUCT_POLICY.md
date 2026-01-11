# Product Policy

This document is binding for product behavior and is the single reference for
core playback/recording semantics.

## Policy

- Live means Live + DVR (default behavior).
- DVRWindow default: 4h.
- VOD includes only completed recordings.
- No in-progress VOD.
- No automatic DVR -> VOD transition.

## Rationale

- Prevents feature drift.
- Ends ambiguous discussions ("but during recording...").
- Gives developers and users a fixed reference point.
