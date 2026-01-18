# xg2g WebUI Design System - Broadcast Console 2026

**Version:** 2.0 (Broadcast-Console)  
**Last Updated:** 2026-01-18  
**Status:** Production  
**Philosophy:** Professional broadcast console - precision over spectacle

> This document defines the formal design contract for the xg2g WebUI. All UI changes must conform to these rules to maintain consistency and quality.

---

## Table of Contents

1. [2026 Token Set](#2026-token-set)
2. [Typography Contract](#typography-contract)
3. [Color System](#color-system)
4. [Surface Treatment](#surface-treatment)
5. [Motion System](#motion-system)
6. [Component Primitives](#component-primitives)
7. [Accessibility](#accessibility)
8. [Playback Governance](#playback-governance)
9. [Contract Rules & Stop Criteria](#contract-rules--stop-criteria)
10. [Review Checklist](#review-checklist)

---

## 2026 Token Set - Broadcast Console

> **CTO Contract:** These tokens are the source of truth. No hardcoded colors or magic numbers. Violations break the design system.

### Typography Stack

#### Font Families

```css
/* Display & Headings - Technical, Modern */
--font-heading: 'Space Grotesk', -apple-system, sans-serif;

/* Body & UI - Professional, Neutral */
--font-body: 'IBM Plex Sans', -apple-system, sans-serif;

/* Technical Data - Tabular, Monospace */
--font-mono: 'JetBrains Mono', 'SF Mono', 'Courier New', monospace;
```

**Variable Font Ranges:**

- Space Grotesk: 400-700
- IBM Plex Sans: 400-600  
- JetBrains Mono: 400-500

#### Type Scale (16px Base)

```css
/* Display */
--text-display: 2rem;      /* 32px - Dashboard title */
--text-h1: 1.5rem;         /* 24px - Section headers */
--text-h2: 1.25rem;        /* 20px - Card titles */
--text-h3: 1rem;           /* 16px - Subsection labels */

/* Body */
--text-base: 1rem;         /* 16px - Standard body */
--text-sm: 0.875rem;       /* 14px - Secondary info */
--text-xs: 0.75rem;        /* 12px - Metadata, captions */

/* Technical/Mono */
--text-mono-base: 0.875rem;  /* 14px - IPs, timestamps */
--text-mono-sm: 0.75rem;     /* 12px - Technical IDs */
```

#### Font Features

```css
/* Tabular numbers for technical data */
.tabular {
  font-feature-settings: 'tnum' 1;
  font-variant-numeric: tabular-nums;
}

/* Standard ligatures for body text */
.body-text {
  font-feature-settings: 'liga' 1, 'calt' 1;
}
```

### Color System - Warm Neutrals + Dual Accent

#### Background Layers (Warm Shift)

```css
--bg-base: #0d0e12;        /* Slightly warm black base */
--bg-elevated: #16171d;    /* Card/panel backgrounds */
--bg-overlay: #1d1f26;     /* Modals, popovers */
--bg-hover: #24262e;       /* Interactive hover states */
--bg-input: #1a1c23;       /* Form input backgrounds */
```

**Rationale:** Warmer than pure slate (#0f172a) - less clinical, better for long operator sessions.

#### Text Hierarchy

```css
--text-primary: #f5f5f7;   /* High contrast - headings, key data */
--text-secondary: #a1a5b3; /* Standard UI labels, body text */
--text-tertiary: #6b7080;  /* Metadata, timestamps, hints */
--text-disabled: #4a4d5a;  /* Disabled states */
```

**Contrast Ratios (WCAG AAA):**

- Primary on Base: 16:1
- Secondary on Base: 8:1
- Tertiary on Base: 4.8:1

#### Dual-Accent Semantic System

**Blue = Action/Interactive**

```css
--accent-action: #4a90f5;           /* Primary CTA, links, focus */
--accent-action-hover: #5c9ff7;     /* Hover state */
--accent-action-pressed: #3a7fe3;   /* Active/pressed state */
--accent-action-subtle: rgba(74, 144, 245, 0.12);  /* Subtle backgrounds */
```

**Amber = Live/Recording (Critical Status)**

```css
--accent-live: #f59f0a;             /* LIVE broadcasts, REC indicator */
--accent-live-hover: #f7b035;       /* Hover state */
--accent-live-pulse: #fbbf33;       /* Pulse animation glow */
--accent-live-subtle: rgba(245, 159, 10, 0.12);
```

**Semantic States**

```css
--status-success: #34d399;  /* Healthy, connected, operational */
--status-warning: #fbbf24;  /* Caution, partial data, degraded */
--status-error: #f87171;    /* Failed, disconnected, critical */
--status-info: #60a5fa;     /* Neutral information */
```

**Usage Rules:**

- Blue for ALL user actions (buttons, links, navigation)
- Amber ONLY for live/recording states (never decorative)
- Green for success (health checks, connection status)
- Red for errors (failures, disconnects)

#### Borders & Dividers

```css
--border-base: rgba(255, 255, 255, 0.08);      /* Standard borders */
--border-elevated: rgba(255, 255, 255, 0.12);  /* Emphasized borders */
--border-focus: var(--accent-action);          /* Focus rings */
```

### Surface Treatment - Subtle & Technical

#### Shadow System (Refined)

```css
/* Level 1 - Resting (Standard Cards) */
--shadow-card: 
  inset 0 1px 0 rgba(255, 255, 255, 0.03),  /* Top highlight */
  0 2px 8px rgba(0, 0, 0, 0.4);             /* Subtle lift */

/* Level 2 - Hover (Elevated) */
--shadow-hover: 
  inset 0 1px 0 rgba(255, 255, 255, 0.04),
  0 4px 16px rgba(0, 0, 0, 0.5);

/* Level 3 - Active/Accent (Live/Recording Cards) */
--shadow-accent-live: 
  inset 0 1px 0 rgba(245, 159, 10, 0.15),
  0 4px 16px rgba(245, 159, 10, 0.12);

--shadow-accent-action: 
  inset 0 1px 0 rgba(74, 144, 245, 0.15),
  0 4px 16px rgba(74, 144, 245, 0.12);
```

**No Glow on Standard Elements** - reserved for status only

#### Card Patterns

**Standard Card:**

```css
background: var(--bg-elevated);  /* Solid only, no gradients */
border: 1px solid var(--border-base);
border-radius: 12px;
box-shadow: var(--shadow-card);
```

**Accent Card (Live/Recording):**

```css
border-color: var(--accent-live);
box-shadow: var(--shadow-accent-live);
```

**Hover State:**

```css
transform: translateY(-2px);  /* Subtle lift, not -4px */
box-shadow: var(--shadow-hover);
border-color: var(--border-elevated);
/* NO GLOW - hierarchy preserved */
```

### Background Texture - Grain/Scanline

**CTO Performance Guard:** Opacity ≤ 0.04, GPU-friendly, no repaint on scroll.

```css
/* Scanline effect */
body::before {
  content: '';
  position: fixed;
  inset: 0;
  background-image: 
    repeating-linear-gradient(
      0deg,
      rgba(255, 255, 255, 0.015) 0px,
      transparent 1px,
      transparent 2px
    );
  pointer-events: none;
  opacity: 0.5;
  z-index: -1;
}

/* Grain/noise texture */
body::after {
  content: '';
  position: fixed;
  inset: 0;
  background-size: 200px 200px;
  opacity: 0.03;  /* Hard limit: ≤ 0.04 */
  pointer-events: none;
  mix-blend-mode: overlay;
  z-index: -1;
}
```

**Feature Flag Ready:** If Safari/iPad performance degrades, disable via CSS class.

---

## Motion System - Purposeful Only

> **CTO Contract:** Continuous motion is **FORBIDDEN** except status pulse (LIVE/REC).

### Allowed Animations

#### 1. Entrance (One-Time, Page Load)

> **CTO Rule:** All entrance animations must be defined in `index.css` as global utility classes. Feature-specific CSS is forbidden from defining or calling animations.

```css
/* index.css */
@keyframes enterFade {
  from { opacity: 0; }
  to { opacity: 1; }
}

.animate-enter {
  animation: enterFade 200ms ease-out forwards;
}
```

**Rules:**

- Duration: 200ms (fixed)
- Easing: ease-out
- No transform (GPU-friendly)
- Once per page load only

#### 2. Interactions (User Feedback)

```css
transition-duration: 180ms;  /* Range: 160-220ms */
transition-timing-function: cubic-bezier(0.4, 0, 0.2, 1);
```

**Applied to:**

- Button press
- Card hover
- Navigation selection
- Focus states

#### 3. Status Pulse (Live/Recording ONLY)

```css
@keyframes statusPulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.6; }
}

.status-chip[data-state="live"] .status-chip__icon,
.status-chip[data-state="recording"] .status-chip__icon {
  animation: statusPulse 2s ease-in-out infinite;
}
```

**Exclusive to:** Live broadcasts, active recordings, critical errors

### Forbidden Animations

 **Removed in 2.0:**

- Background gradient animation (15s)
- Progress bar shimmer (3s)
- Button ripple effects
- Card entrance translateY
- Continuous decorative motion

### Accessibility (Global Guard)

> **CTO Rule:** `prefers-reduced-motion` is managed centrally in `index.css`. Feature CSS MUST NOT duplicate these blocks.

```css
/* index.css */
@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after {
    animation-duration: 0.01ms !important;
    animation-iteration-count: 1 !important;
    transition-duration: 0.01ms !important;
  }
}
```

**Must be present in ALL CSS files.**

---

## Component Patterns - Canonical

> **CTO Contract:** These patterns are the product standard. Deviations require design review.

### Status Chips (No Emojis)

**Structure:**

```html
<span class="status-chip" data-state="live">
  <span class="status-chip__icon">●</span>
  <span class="status-chip__label">LIVE</span>
</span>
```

**States:** `live`, `recording`, `idle`, `error`, `active`

**CSS:**

```css
.status-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 10px;
  border-radius: 6px;
  font-family: var(--font-body);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.5px;
  text-transform: uppercase;
}

.status-chip[data-state="live"] {
  background: var(--accent-live-subtle);
  color: var(--accent-live);
  border: 1px solid var(--accent-live);
}

.status-chip[data-state="live"] .status-chip__icon {
  animation: statusPulse 2s infinite;
}
```

**Icon Mapping:**

- `●` (U+25CF) = LIVE, RECORDING
- `○` (U+25CB) = IDLE
- `✓` (U+2713) = SUCCESS
- `⚠` (U+26A0) = WARNING
- `✗` (U+2717) = ERROR

### System Health Panel (Compact)

**Layout:** 3-5 tiles, horizontal, monospace numbers

```

 Receiver │ EPG │ Streams │ Rec │ Up   │
    ✓     │  ✓  │    2    │  ●  │ 14h  │

```

**CSS:**

```css
.health-panel {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(80px, 1fr));
  gap: 1px;
  background: var(--border-base);
  border-radius: 12px;
  overflow: hidden;
}

.health-tile {
  background: var(--bg-elevated);
  padding: 12px;
  text-align: center;
}

.health-tile__value {
  font-family: var(--font-mono);
  font-size: var(--text-h2);
  font-feature-settings: 'tnum' 1;
}
```

### Navigation - Vertical Rail

**Layout:** Left-side rail, 60px wide, icon + label on hover

```

  ●   │  Quick Actions

  ⌂   │  Dashboard
  ≡   │  Channels (active: inset bar)
  ◷   │  EPG
  ⏺   │  Recordings
 │  Settings (inset bar = active)│  

```

**Active State:** Inset vertical bar (3px, accent-action), NOT glow

**CSS:**

```css
.nav-rail {
  width: 60px;
  background: var(--bg-elevated);
  border-right: 1px solid var(--border-base);
}

.nav-item {
  position: relative;
  padding: 16px;
  transition: background 180ms;
}

.nav-item[aria-current="page"]::before {
  content: '';
  position: absolute;
  left: 0;
  top: 8px;
  bottom: 8px;
  width: 3px;
  background: var(--accent-action);
  border-radius: 0 2px 2px 0;
}
```

---

## Migration from v1.0 to v2.0

### Breaking Changes

1. **Fonts:** Inter → Space Grotesk (headings) + IBM Plex Sans (body)
2. **Colors:** Cold slate → Warm neutrals
3. **Motion:** Continuous animations removed
4. **Cards:** Gradients removed, solid backgrounds
5. **Navigation:** Top bar → Vertical rail

### Compatibility

- Reduced-motion support: ✅ Maintained
- Touch targets (44px): ✅ Maintained
- Contrast ratios: ✅ Improved (WCAG AAA)

---

## Maintenance Rules

### Before Adding Features

1. **Check tokens first** - use existing, never hardcode
2. **Verify motion budget** - entrance + status only
3. **Test reduced-motion** - must work with animations off
4. **Confirm pattern exists** - chips/panels/rail defined?

### Design Review Stop Criteria

 **Reject if:**

- Hardcoded hex colors outside DESIGN.md
- Continuous animation without status purpose
- Glow on non-status elements
- Typography outside defined stack
- Motion duration > 220ms for interactions

 **Approve if:**

- Uses documented tokens
- Follows canonical patterns
- Respects motion budget
- Maintains accessibility

---

**Version History:**

| Version | Date | Changes |
|---------|------|---------|
| 2.0 | 2026-01-18 | Broadcast-Console design system |
| 1.0 | 2026-01-18 | Initial design system |

---

---

## Playback Governance

> **CTO Contract:** Playback is the core mission. Logic must be deterministic, backend-driven, and browser-aware.

### Browser Support Matrix (HLS Path)

To ensure stability, we actively manage the HLS stack instead of relying on default browser behavior:

| Platform | Recommended Browser | HLS Engine | Rationale |
|----------|-------------------|------------|-----------|
| **macOS / iOS** | Safari (Native) | **Native HLS** | MSE on iOS is historically unstable; native provides best battery/performance. |
| **Windows / Linux** | Chrome / Edge / FF | **HLS.js** | Consistent seeking, DVR buffers, and error recovery across Chromium/Gecko engines. |
| **Mobile Android** | Chrome | **HLS.js** | Standardized behavior for DVR window management. |

### "Stats for Nerds" (First-Class Observability)

The Stats Overlay is a **critical observability tool**, not a feature accessory.

1. **Rule:** Technical data must be raw and unformatted (processed by backend, only displayed by UI).
2. **UI:** Must use `Card`, `.tabular` (monospace), and `StatusChip`.
3. **Copy-Friendliness:** Metrics must be easy to select and copy for bug reports.
4. **Payload Requirement:** Must include `Session-ID` and `Request-ID` to allow direct log/metric correlation.

### Strategy: Thin Client Playback

1. **No Decision Engine:** The WebUI does NOT decide between "Direct Play" vs "Transcode". It follows the `PlaybackInfo` DTO provided by the backend.
2. **Browser Choice Exception:** The WebUI MAY choose the HLS implementation (Native HLS vs HLS.js) based on browser platform for stability.
3. **Hard Stop:** Logic related to quality, variants, or transcoding decisions is strictly FORBIDDEN in the frontend.
4. **Modular Hooks:** Playback state is managed via hooks (`usePlaybackSession`) to keep views pure.

---

## Phase 3: Backend Truth Hardening (Priorities)

> **CTO Objective:** Finalize the "Backend = Truth" contract by closing DTO gaps.

### 1. Extended Stream States

Extend `StreamSession.state` from a simple binary to a semantic lifecycle:

- `starting` | `buffering` | `active` | `stalled` | `ending` | `idle` | `error`

### 2. DVR Window Truth

`PlaybackInfo` must authoritatively deliver:

- `dvr_window_seconds`: Total lookback duration.
- `is_seekable`: Boolean flag for UI timeline enablement.
- `live_edge_unix`: The absolute timestamp of the live edge.

### 3. Recording Status Consolidation

Consolidate multiple internal flags into a single `Status` enum:

- `pending` | `recording` | `completed` | `failed` | `deleting`

### 4. Observability Contract

Ensure `Session-ID` and `Request-ID` are passed to the frontend in all playback DTOs for 1:1 trace correlation in the "Stats for Nerds" block.

---

---

## Contract Rules & Stop Criteria

### Phase 2F Go/No-Go Criteria (Streams & V3Player)

**GO (Green Light):**

- [x] `V3Player.css` is layout-only (Grid/Flex/Spacing).
- [x] All statuses (buffering, live, error) use `StatusChip`.
- [x] Numbers use `--font-mono` + `.tabular`.
- [x] Inline styles are limited to one-liners for CSS variable passthrough.

**NO-GO (Hard Stop):**

- [ ] **Feature-Drift:** Importing heavy player libraries (Video.js, etc.) instead of native `<video>` + light HLS.js.
- [ ] **Glow Drift:** Adding glows or gradients to the player UI for "aesthetic" reasons.
- [ ] **Logic Drift:** Implementing a "Playback Decision Engine" in the frontend.
- [ ] **Governance Failure:** Bypassing `check-ui-contract.sh` for player-related CSS.
- [ ] **Redundancy:** Adding `prefers-reduced-motion` or animations to feature CSS (must be global).

---

## Review Checklist

```
