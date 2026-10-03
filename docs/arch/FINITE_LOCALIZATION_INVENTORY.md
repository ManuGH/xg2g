# Finite Localization Inventory: iOS, tvOS & WebUI Surfaces

- **Verification Status:** Branch Verification Complete — Ready for Integration Review (Antigravity Handoff to Codex / Manuel).
- **Integration Status:** Branches `feat/ios-string-catalog-foundation` and `feat/webui-translation-key-safety` pending mainline merge into `origin/main`.
- **Reproducible Catalog Totals (Exact Key Counts):**
  - **iOS (`Localizable.xcstrings`):** **417 keys** (417 translated in `de`, 417 translated in `en` — 100% bilingual completeness).
  - **WebUI (`en.json` & `de.json`):** **1,291 leaf keys** in `en.json` and **1,291 leaf keys** in `de.json` (100% key parity under `strictKeyChecks: true`).
- **Surface Verification Evidence:**
  - **iOS/tvOS Test Suite:** `xcodebuild test` ran **695 tests across 95 suites: 0 unexpected failures**, 1 documented pre-existing baseline known issue (`TSPipelineUnitTests.swift:482` when `XG2G_LIVE_RECEIVER_STREAM_URL` is unset).
  - **WebUI Verification:** `AdminErrorHandling.test.tsx` (9 tests passing), `AdminLayout.test.tsx` (4 tests passing), `tsc --noEmit` (0 errors), `npm run build` succeeded (849ms).
  - **Error Presentation Contract:** Primary user-facing error copy is strictly localized via resource catalogs; raw browser/network errors (`Failed to fetch`, HTTP status codes) are kept distinct in secondary diagnostic monospace elements (`data-testid="error-detail"`).
- **Locale Scope:** English (`en`, Development Region) & German (`de`, Target Locale).
- **Preservation Contract:** Enigma2 / OpenWebIF external media broadcast data (EPG programme titles, event summaries, channel broadcast names, stream genres, and receiver bouquet names) are strictly preserved **verbatim** in their broadcast language using `Text(verbatim:)` on iOS and untouched model strings on WebUI. Only application-owned chrome, navigation, buttons, accessibility labels, statuses, error banners, and format strings are localized. The application's synthetic Favorites bouquet is localized by its stable ID (`xg2g-app-favorites`).

---

## 1. Surface Overview & Batch Migration Breakdown

*Note: Per-component string counts below represent estimated user-facing text sites migrated across Batches 1–6; catalog inventory counts above represent the reproducible exact key counts.*

| Surface Domain | Screens / Components | Estimated Migrated Text Sites | Outstanding Application Text | Branch Status | Batch |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **iOS: Settings & Infrastructure** | `SettingsView.swift`, `Theme.swift`, `DeviceCapabilities.swift` | ~65 | 0 | **Verified** | Package 2 |
| **iOS: Error Presentation & Diagnostics** | `UserFacingError.swift`, `APIError.swift`, `ProblemDetails.swift` | ~42 | 0 | **Verified** | Package 3 |
| **iOS: Shell, Navigation & Pairing** | `RootView.swift`, `MiniPlayerBar.swift`, `PadRootView.swift`, `TVRootView.swift` | ~22 | 0 | **Verified** | Batch 1 |
| **iOS: Channel Discovery & Home Hub** | `ChannelListView.swift`, `ChannelRow.swift`, `HomeHubView.swift`, `QuickRailsView.swift`, `SmartSearchResultsView.swift` | ~38 | 0 | **Verified** | Batch 2 |
| **iOS: Guide & EPG Grid** | `GuideView.swift`, `GuideGrid.swift`, `GuideComponents.swift`, `TimeFilterPill.swift` | ~16 | 0 | **Verified** | Batch 3 |
| **iOS: Recordings, Timers & Details** | `RecordingsView.swift`, `TimersView.swift`, `ProgramDetailSheet.swift` | ~44 | 0 | **Verified** | Batch 4 |
| **iOS: Player Overlays & Debug HUD** | `PlayerScreen.swift`, `RecordingPlayerScreen.swift`, `TestTSPlayerScreen.swift`, `OfflinePlayerScreen.swift` | ~28 | 0 | **Verified** | Batch 5 |
| **WebUI: Core Player & Shell** | `AppShell.tsx`, `Dashboard.tsx`, `Settings.tsx`, `EPG.tsx`, `PlayerControls.tsx` | ~145 | 0 | **Verified** | Package 1 |
| **WebUI: Admin & Security Console** | `AdminLayout.tsx`, `DevicesManagementSection.tsx`, `SecuritySettingsSection.tsx`, `ProfileManagementSection.tsx`, `ConcurrencySettingsSection.tsx`, `ParentalControlSection.tsx`, `FamilyManagementSection.tsx`, `AccessTimesSection.tsx`, `AuditNotificationsSection.tsx` | ~236 | 0 | **Verified** | Batch 6 |

**Catalog Leaf Key Totals:** 417 iOS String Catalog keys + 1,291 WebUI JSON leaf keys.  
**Estimated Migrated Text Sites:** ~636 application text locations across both platforms.  
**Branch Implementation Scope:** 100% of agreed Batches 1–6 completed with deterministic test evidence.

---

## 2. Final Audit & Literal Classification

A systematic audit across both repositories classifies all literals into three distinct categories:

### Category A: Application UI Text (Migrated & Verified)
All user-facing application controls, navigation bars, buttons, placeholders, dialogs, error messages, toast notifications, status badges, accessibility labels, and time/date formatters are externalized to native localization catalogs:
- **iOS/tvOS:** Stored in `Localizable.xcstrings` and looked up via SwiftUI String Catalogs.
- **WebUI:** Stored in `apps/webui/src/locales/en.json` and `de.json` and resolved via `useTranslation()` under compile-time `strictKeyChecks: true` key safety.

### Category B: Preserved Source Metadata (Strictly Preserved Verbatim)
These strings originate from external broadcast tuners (Vu+ Uno 4K / OpenWebIF / DVB streams) and are intentionally never modified or passed through translation lookups to avoid domain collision (e.g., a TV show titled "Settings" or "News"):
- **EPG Programme Titles & Summaries:** e.g. `show.title`, `event.title`, `show.shortDescription` (wrapped in `Text(verbatim:)` on iOS / rendered raw in WebUI).
- **DVB Service / Channel Names:** e.g. `service.name`, `Das Erste HD`, `ZDF HD`, `RTL Television`.
- **Receiver Bouquet Names:** e.g. `bouquet.name` as configured on the tuner (except the application's synthetic favorites bouquet, which is localized by its stable ID `AppModel.favoritesBouquetID` / `xg2g-app-favorites`).
- **Codec & Technical Stream Metadata:** e.g. `AC3`, `E-AC3`, `H.264`, `HEVC`, `AAC`, DVB aspect ratios (`16:9`, `4:3`).

### Category C: Technical Identifiers & Protocols (Strictly Preserved Constants)
These string literals represent internal protocol constants, API routes, or cryptographic material that must never be translated:
- **API Endpoints & HTTP Routes:** e.g. `/api/v3/household/devices`, `/stream/live/`, `/auth/login`.
- **RFC 7807 ProblemDetails Type URIs:** e.g. `urn:xg2g:playback:no-tuner`, `urn:xg2g:playback:session-limit`.
- **RBAC Role Constants:** e.g. `'admin'`, `'member'`, `'guest'`.
- **Web Storage Keys:** e.g. `'xg2g_lang'`, `'nav-collapsed-v2'`, `'xg2g.household.selected-profile.v1'`.
- **Cryptographic Material:** SHA-256 hash chains, JWK thumbprints, WebAuthn challenge bytes, and P-256 public keys.

---

## 3. WebUI Error Presentation & Diagnostic Separation Contract

All migrated WebUI administrative and settings surfaces adhere to the strict error presentation contract:
1. **Primary Localized Heading:** Errors display the catalog-backed localized string (e.g. `t('admin.profiles.loadError')`).
2. **Diagnostic Separation:** Underlying raw technical details (`err?.message`, HTTP response statuses, or network failures) are never displayed in place of the localized message. Instead, they are captured in dedicated `errorDetail` state and rendered in secondary diagnostic monospace elements (`data-testid="error-detail"`).
3. **Automated Verification:** Verified in `AdminErrorHandling.test.tsx` across German and English locales with mocked network rejections, 403 Forbidden errors, and 500 Internal Server errors.
