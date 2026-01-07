# Architectural Guidelines & Review Checklist

> [!TIP]
> Use this checklist for every PR. Violations block merging.

## 1. No UI Drift (Thin Client)

- [ ] Does this PR add logic to `V3Player.tsx`? **REJECT** usually.
- [ ] Does it retry/fallback on error? **REJECT**.
- [ ] Does it use `localStorage` for logic? **REJECT**.

## 2. No Profile Remnants

- [ ] grep `profile`, `safari`, `auto` in the codebase.
- [ ] If found outside of comments explaining removal: **REJECT**.

## 3. Config Hygiene

- [ ] Does it add a new config key?
  - [ ] Is it necessary?
  - [ ] Is it documented in `CONFIGURATION.md`?
  - [ ] Does it have a safe default?

## 4. Universal Policy Compliance

- [ ] Does the change affect FFmpeg flags?
- [ ] Does it deviate from H.264/AAC/fMP4?
- [ ] If yes, where is the RFC/ADR?

## 5. Documentation First

- [ ] Is `README.md` still accurate?
- [ ] Is `CHANGELOG.md` updated?
