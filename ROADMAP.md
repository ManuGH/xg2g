# xg2g Roadmap 2026

## Vision: User Experience & Resilience

After solidifying the core stability (HLS, Circuit Breaker) and security (TLS, XML Hardening) in v2.2.0, the focus shifts to **User Experience (UX)** and **Operational Resilience**. The goal is to make `xg2g` not just a robust backend tool, but a user-friendly application that is easy to manage and monitor.

## Q1 2026: The "UX" Update (v3.0.0)

### 1. Modern WebUI (Priority: High) - ✅ Completed in v3.0.0

**Goal:** Replace the need for manual config file editing and provide real-time visibility.

- **Design:** See [WEBUI_DESIGN.md](file:///Users/manuel/xg2g/WEBUI_DESIGN.md) for detailed architecture and mockups.
- **Walkthrough:** See [WEBUI_WALKTHROUGH.md](file:///Users/manuel/xg2g/docs/WEBUI_WALKTHROUGH.md) for usage guide.
- **Dashboard:** Live stats (active streams, bandwidth, error rates).
- **Configuration:** Visual editor for `config.yaml` (mappings, profiles).
- **Stream Browser:** Preview streams directly in the browser.
- **Logs:** Real-time log viewer with filtering.
- *Tech Stack:* Embedded React/Vue app or HTMX + Go templates (keeping binary size small).

### 2. EPG Fallback Sourcing (Priority: Medium)

**Goal:** Ensure EPG data availability even when the primary receiver is down or has gaps.

- **External Sources:** Support for external XMLTV URLs (e.g., gz/xz compressed).
- **Merge Logic:** Smart merging of receiver EPG and external sources.
- **Cache Persistence:** Disk-based caching of EPG data to survive restarts.

### 3. Auto-Update System (Priority: Low)

**Goal:** Simplify maintenance for non-Docker users.

- **Self-Update:** `xg2g update` command to fetch the latest binary from GitHub Releases.
- **Update Channel:** Stable vs. Beta channels.

## Technical Debt & Maintenance

### 1. Test Consolidation (Option C)

**Goal:** Reduce maintenance burden and improve test reliability.

- **Consolidate:** Merge scattered tests in `internal/proxy`, `internal/api`, and `test/` into a coherent suite.
- **Mocking:** Standardize on a single mocking strategy (currently mix of manual mocks and `httptest`).

### 2. Legacy Cleanup

**Goal:** Remove unused code and simplify architecture.

- **Refactor `internal/jobs`:** Modernize the bouquet/service fetching logic (remove global state).
- **Remove Deprecated:** Final removal of any v1.x legacy features.

## Timeline Draft

| Milestone | Focus | ETA |
|-----------|-------|-----|
| **v2.3.0** | Test Consolidation & Cleanup | Jan 2026 |
| **v2.4.0** | EPG Fallback Sourcing | Feb 2026 |
| **v3.0.0** | WebUI & Auto-Update | Mar 2026 |
