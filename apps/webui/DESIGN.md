# xg2g WebUI Design Guide

**Version:** 3.0
**Last reviewed:** 2026-10-06 (against `main` @ `78f3e62b`, desktop 1440×900 and phone 390×844)

This guide holds the principles and rules for the WebUI. It deliberately holds
**no token values**. Colors, sizes, shadows and durations live only in
[`src/index.css`](src/index.css). A guide that copies values drifts away from
the code. Version 2.x did exactly that (see [History](#history)).

Every rule names the gate that enforces it. A rule marked **unenforced** is a
candidate for a gate. Until it has one, reviewers check it by hand.

---

## 1. What the UI is for

| | |
|---|---|
| **Who** | A household watching satellite TV in a browser on a couch, phone, tablet or desktop. Plus one operator who sets the system up. |
| **Primary job** | Get to a moving picture in as few steps as possible: a live channel, a recording, or continue watching. |
| **Secondary job** | Plan recordings (timers, series rules). |
| **Tertiary job** | Operate the server: settings, system status, logs, playback diagnostics. |

That split defines two kinds of surfaces:

- **Household surfaces** are Start, TV guide, Recordings, the player and the
  watch page. They are content-first. They never show transport or system
  vocabulary: codecs, transcode profiles, service references, session or
  request IDs, rule IDs, mount names.
- **Operator surfaces** are Settings, System, Logs and Stats for Nerds. They
  may be dense and technical. Raw values must stay selectable and copyable.

## 2. Principles

The identifiers exist so that reviews can cite a principle ("violates P3").
They are not a ranking.

- **P1 · The picture is the colour.** Channel logos, recording thumbnails and
  video carry the chroma. Chrome is neutral and quiet. An accent colour
  appears only where it means something (see §3.2).
- **P2 · Content in the first viewport.** On a household surface, the first
  viewport shows content (a channel row, a recording, a programme) at both
  reference sizes. Headers, counters and filters do not count. A filter bar is
  one row on desktop and collapses behind one control on a phone.
- **P3 · One surface level per region.** Use at most page → surface →
  overlay. Never put a card inside a card inside a panel. Group with spacing
  and alignment before borders, and with borders before nested containers.
- **P4 · Opaque by default.** Translucency and `backdrop-filter` are reserved
  for chrome that sits on top of moving video: player controls, the channel
  switcher, the mini-player. Everything else is opaque.
- **P5 · Destructive actions are never the loudest element.** Delete sits
  behind an overflow menu or a secondary button and always confirms through
  `UiOverlayProvider`. It is never a full-width filled red button next to
  routine actions.
- **P6 · Say it in the household's words.** Name things by what people
  watch, not by how the system is built (see §7).
- **P7 · Every route is a place.** Anything a person can navigate to,
  including a recording's watch page, has its own URL. Browser back returns
  to where they came from. Entering a new place resets scroll.

## 3. Tokens

### 3.1 Architecture

`src/index.css` is the single source of truth. Tokens come in three tiers.
New tokens must fit one of them:

| Tier | Purpose | Examples | May be used by |
|---|---|---|---|
| **Primitive** | Raw scale steps, no meaning | type steps, radius steps, spacing steps, palette | semantic tokens only |
| **Semantic** | A role in the UI | `--bg-*`, `--surface-*`, `--text-*`, `--border-*`, `--accent-*`, `--status-*`, `--motion-*`, `--radius-*` | all feature CSS |
| **Component** | A value the semantic tier cannot express | `--player-*`, `--channel-switcher-*` | that component only |

Rules:

- Add a component token only when a semantic token cannot express the value.
  Don't add a component-specific shadow or gradient per feature: index.css has
  45 shadow and 18 gradient tokens, most of them one-offs.
- Author new colour tokens in `oklch()`. Derive tints with
  `color-mix(in oklch, …)` instead of hand-writing another rgba variant.
- Scale every font size, spacing step and radius through the user scale
  variables (`--ui-font-scale`, `--ui-density-scale`). The Display settings
  depend on it.
- Shared tokens with the Android/iOS apps are **out of scope** for now. Each
  native app has its own theme (`android/…/ui/theme/Theme.kt`, iOS assets).
  A shared token source, for example the W3C Design Tokens (DTCG) format
  generating CSS, Kotlin and Swift, is only worth it once the apps are meant
  to look the same. That is a decision to make first, not a default.

### 3.2 Colour semantics

| Token family | Meaning | Allowed on |
|---|---|---|
| `--accent-action*` | Something the user can do: primary button, link, selection, focus | interactive elements |
| `--accent-live*` | Live: the now-line in the guide, the LIVE chip, a live session | live state only |
| `--status-error` / recording red | Recording in progress, and errors | REC indicators, error states |
| `--status-success` / `--status-warning` / `--status-info` | Health of a system component | operator surfaces, status chips |
| `--bg-video-stage` | True black behind video, so letterbox bars vanish on OLED/XDR | video stages only |

Accent colour is never decoration: no gradient washes, no glow on
non-status elements, no accent-tinted panels. **Gate:** hex literals outside
`index.css` are blocked by `scripts/verify-no-hardcoded-colors.sh`.
`rgba()`, `hsl()` and `oklch()` literals in feature CSS are **unenforced**
(14 occurrences in 4 files today).

### 3.3 Typography

- **Family:** the platform UI font (`--font-body`, `--font-heading`). There is
  no web font download, and text renders natively on Apple, Android and
  Windows. `--font-mono` is for operator data only (logs, Stats for Nerds,
  IDs). It is never used for dates, times or durations on household surfaces.
- **Scale:** every `font-size` uses a `--text-*` token. A one-off size is a
  missing step in the scale, not a local exception. **Unenforced** (75
  distinct `font-size` values and 77 hard-coded `px` sizes today).
- **Reading distance:** household surfaces are read from a couch. Body text
  there must not go below the body step of the scale. The current body step
  of 13 px is too small for that context (see §9).
- **Figures:** times, durations and counts use `font-variant-numeric:
  tabular-nums` so columns and the guide's time ruler do not jitter.
- **Durations:** one format everywhere. Use `h:mm:ss` for anything over an hour
  and `m:ss` below that. Never write `179:55` in one place and `2:59:55` in
  another.
- **Case:** sentence case everywhere. No all-caps eyebrow labels above
  headings, and no `text-transform: uppercase` for decoration.
- **Line length:** prose and descriptions are capped at about 75ch.

### 3.4 Shape and space

- **Radius:** use the `--radius-*` steps only. The radius encodes hierarchy:
  larger containers get larger radii, controls get smaller ones. **Unenforced**
  (30 distinct `border-radius` values today).
- **Spacing:** a spacing step scale belongs in `index.css`. It does not exist
  yet (0 spacing tokens, 125 distinct `padding` values). Until it does, new
  CSS reuses the values already used on the same surface.
- **Breakpoints:** use a small fixed set. There are 12 different `max-width`
  and `min-width` values today. For components that appear in more than one
  context (a recording card in the library grid and in "more episodes"), use
  **container queries** instead of viewport breakpoints.
- **Safe areas:** fixed and floating chrome respects
  `env(safe-area-inset-*)`. Floating bottom navigation reserves its height in
  `scroll-padding-bottom` so focused content is never hidden behind it
  (WCAG 2.4.11).

## 4. Components

Feature code builds from the primitives in
[`src/components/ui`](src/components/ui): `Button`, `ButtonLink`, `Card`,
`StatusChip`, `EmptyState`. Create a new primitive when the same pattern
appears in a third feature, not before.

- **Buttons:** use the `Button` primitive. **Gate:**
  `scripts/verify-no-btn-classes.sh` blocks feature-level `btn-*` classes.
  Each view has exactly one primary action. A sign-in screen with two filled
  primary buttons violates this.
- **Dialogs:** use `UiOverlayProvider` confirm and toast. **Gate:**
  `scripts/verify-no-window-dialogs.sh`.
- **Icons:** one SVG icon set: a 24-unit grid, `currentColor` and a 1.8 stroke.
  The style is defined by `NavIcon` in `Navigation.tsx` and
  `playerControlGlyphs.tsx`. Emoji and Unicode glyphs (📺 📁 ⚡ ☰ ▦ ⏱ ✓ ⚠)
  are never used as icons. **Unenforced.**
- **Status chips:** state is carried by colour *and* shape, so it never relies
  on colour alone. The accessible name is fully localised. Today it
  concatenates an English state key.
- **Cards:** a card is a hit target or a grouping, never both decoration and
  container. Actions on media cards (edit, delete) appear on hover or focus,
  or in an overflow menu, and are not permanently stamped on every thumbnail.

## 5. Motion

Motion answers a user action or signals live state. Otherwise there is none.

- **Allowed:** feedback on interaction (`--motion-standard`, within the
  160–220 ms band); a status pulse for live and recording; loading indicators;
  one entrance per page load (`.animate-enter`, opacity only).
- **Not allowed:** decorative continuous motion, shimmer, staggered entrances
  and hover lifts on every card.
- **Gate:** `scripts/verify-motion-contract.sh` blocks `@keyframes` and
  `animation` outside `index.css` (except `statusPulse`). It checks *where*
  motion is defined, not *what* it is. `index.css` currently holds 19
  keyframes, including shimmer and spin effects for player start-up. Each one
  needs a functional reason or should go.
- `prefers-reduced-motion` is handled once, globally, in `index.css`. Feature
  CSS does not repeat it.

## 6. Accessibility baseline

The bar is **WCAG 2.2 AA**, verified rather than claimed.

- **Contrast:** text needs 4.5:1, large text 3:1, and UI components and focus
  indicators 3:1. Measure on the surface the text actually sits on. A
  translucent panel over a gradient is not `--bg-base`. Disabled controls
  still need a clear accessible name.
- **Focus:** every interactive element has a visible `:focus-visible` style
  that doesn't rely on `box-shadow` alone (it vanishes in
  `forced-colors` mode). Focus is never hidden behind sticky or floating
  chrome (2.4.11).
- **Targets:** at least 24×24 CSS px (2.5.8), and 44×44 on touch-first
  surfaces (player controls, bottom navigation, guide rows on a phone).
- **Structure:** a `role="tablist"` contains only `role="tab"` children, and
  every icon-only button has a localised `aria-label`.
- **User preferences:** respect `prefers-reduced-motion` (central),
  `prefers-reduced-transparency` where the browser supports it (fall back to
  opaque surfaces, see P4), and `forced-colors` (borders and focus must stay
  visible).
- **TV and remote:** the Android TV host uses native navigation
  (`hostEnvironment.platform === 'android-tv'`). On any other big screen the
  web UI must be fully usable with arrow keys, Enter and Back, in a logical
  focus order.
- **Tests:** `tests/Navigation.a11y.test.tsx`. Adding an automated axe pass
  for household surfaces is **unenforced** (planned).

## 7. Writing

- **Language:** German first, English second. Every user-visible string goes
  through `t()`. That includes `aria-label`, `title` and `placeholder`.
  Parity between `de.json` and `en.json` is checked by
  `src/locales/locales.parity.test.ts`. Literal strings in JSX are
  **unenforced** today (for example `SeriesManager.tsx`, `EPG.tsx`,
  `V3PlayerView.tsx` and `Navigation.tsx` still carry them).
- **Voice:** plain verbs, sentence case and no filler. A button says what
  happens ("Aufnahme löschen", not "OK"). An action keeps its name through
  the flow: the button "Aufnehmen" leads to the toast "Aufnahme geplant".
- **Errors:** say what happened and what to do next. Errors don't apologise,
  never show a bare status code on household surfaces, and take over the stage
  they block. A failed player shows the error and the way out, not a small
  pill under a still-active play button. See
  [`docs/arch/ERROR_PRESENTATION_SPECIFICATION.md`](../../docs/arch/ERROR_PRESENTATION_SPECIFICATION.md).
- **Empty states:** an empty state is an invitation to act. If there is
  nothing to act on, don't render the section at all.
- **Internal markers stay internal.** Rule tags, IDs and raw references that
  the backend stores in descriptions or names must be stripped or resolved
  before display. Resolve a service reference to a channel name and logo, and
  never fall back to the raw reference on a household surface.

### Household vocabulary

| Use | Avoid on household surfaces |
|---|---|
| Fernsehen / TV guide | EPG, EPG-Übersicht, Live-Programm |
| Aufnahmen | DVR, DVR-Mediathek, Elemente |
| Läuft gerade / Wiedergaben | Operator-Sitzungen, Sessions |
| Sender | Service-Referenz, Service-Ref |
| Senderliste | Bouquet (allowed on operator surfaces) |
| Weiter schauen | Resume-Position |

## 8. Playback boundary

The UI renders what the backend decides. It does not decide.

- It follows the backend's playback DTOs and has no client-side decision
  engine for direct play, transcode, quality or variants. It may pick the HLS
  implementation (native HLS vs hls.js) for stability.
- **Gates enforced in CI** (as tests in `webui-test`, part of `make ci-pr`):
  `gate:seekable-contract`, `gate:mode-bridge`.
- **Gates defined but not wired into CI** (2026-10-06):
  - Passing: `gate:no-ua-sniffing`, `gate:no-raw-error-text`.
  - **Failing on `main`:** `gate:no-client-decision-engine` (11 hits),
    `gate:no-duration-guessing` (5), `gate:no-seek-resume-guessing` (13),
    `gate:no-raw-json-fetch` (11).
  - The hits are not yet classified as real violations or heuristic false
    positives. Until each hit is fixed or allow-listed with a reason and the
    gates run in CI, these rules are **unenforced**.
- The specification lives in
  [`docs/arch/PLAYBACK_DECISION_SPEC_INDEX.md`](../../docs/arch/PLAYBACK_DECISION_SPEC_INDEX.md).
  This guide only covers how playback looks: a true-black stage, chrome per
  P4, errors per §7.

## 9. Known drift and migration order

Measured on 2026-10-06. This is the work that brings the code in line with
this guide, in order. Each step is its own PR. Steps that touch player mount
or unmount need the lifecycle test matrix from `AGENTS.md`.

1. **Correctness on household surfaces:**
   - Localise `SeriesManager` and the remaining JSX literals.
   - Resolve the series-rule channel to its name.
   - Strip rule markers from recording descriptions.
   - Fix the Start "Recorder" tile, which shows a programme title next to
     "inactive".
2. **Watch page as a route (P7):** a recording's watch page gets its own URL,
   browser back works, and scroll resets.
3. **First viewport (P2, P3):**
   - Collapse the Recordings header (title, filters and sort in one row, no
     counter tiles) and the TV guide toolbar into one row each.
   - In the guide, show record actions on hover or focus only.
   - Remove the duplicate entry points (Timers, sidebar toggles).
4. **Token pass:**
   - Add spacing, radius and type step scales, and gates that block literals
     outside them.
   - Raise the household body size.
   - Make non-video surfaces opaque (P4).
   - Replace emoji and glyph icons with the SVG set and add a gate for them.
   - Retire unused one-off shadow and gradient tokens.
5. **Settings structure:** a section list plus a detail pane on desktop, list →
   detail on a phone, and no near-empty landing section.

When a step lands, update this section in the same PR.

## History

| Version | Date | Change |
|---|---|---|
| 3.0 | 2026-10-06 | Rewritten. Principles and gates instead of values. Added the household/operator split, WCAG 2.2 AA baseline, writing rules, token tiers and the measured drift with migration order. Removed stale claims (Space Grotesk/IBM Plex, a 16 px base, "no gradients", a 60 px rail), the "Phase 3 Backend Truth Hardening" backlog (its DTO fields are in the API: `isSeekable`, `dvrWindowSeconds`, `liveEdgeUnix`, `stalled`, `requestId`/`sessionId`), and the v1→v2 migration notes. The playback section now points to the gates and the architecture spec. |
| 2.1 | 2026-05-24 | Token values moved to `src/index.css`. |
| 2.0 | 2026-01-18 | "Broadcast Console" system. |
| 1.0 | 2026-01-18 | Initial version. |
