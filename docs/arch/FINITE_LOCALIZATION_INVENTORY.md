# Finite Localization Inventory: iOS, tvOS & WebUI Surfaces

- **Status:** All Batches 1–6 Completed & Deterministically Verified across iOS, tvOS, and WebUI.
- **Branches:** `feat/ios-string-catalog-foundation` (iOS/tvOS) & `feat/webui-translation-key-safety` (WebUI)
- **Base Verification:**
  - **iOS/tvOS:** 26/26 iOS Playback & Localization Tests Passing; tvOS Scheme Build Succeeded; visual artifacts generated for DE/EN.
  - **WebUI:** 154/154 Test Suites Passing (863/863 Tests Passing); TypeScript strict key contract (`tsc --noEmit`) 0 errors; Production Build (`npm run build`) succeeded in 745ms.
  - **Bilingual Rendered Verification:** Dedicated rendered tests in `AdminLayout.test.tsx` verifying each of the 10 reachable sections in both DE and EN against real translation catalogs.
- **Locale Scope:** English (`en`, Development Region) & German (`de`, Target Locale)
- **Preservation Contract:** Enigma2 / OpenWebIF external media broadcast data (EPG programme titles, event summaries, channel broadcast names, stream genres, and receiver bouquet names) are strictly preserved **verbatim** in their broadcast language using `Text(verbatim:)` on iOS and untouched model strings on WebUI. Only application-owned chrome, navigation, buttons, accessibility labels, statuses, and format strings are localized.

---

## 1. Surface Overview & Progress Summary

| Surface Domain | Screens / Components | Verified Strings | Outstanding | Status | Batch |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **iOS: Settings & Infrastructure** | `SettingsView.swift`, `Theme.swift`, `DeviceCapabilities.swift` | 65 | 0 | **Verified** | Package 2 |
| **iOS: Error Presentation & Diagnostics** | `UserFacingError.swift`, `APIError.swift`, `ProblemDetails.swift` | 42 | 0 | **Verified** | Package 3 |
| **iOS: Shell, Navigation & Pairing** | `RootView.swift`, `MiniPlayerBar.swift`, `PadRootView.swift`, `TVRootView.swift` | 22 | 0 | **Verified** | Batch 1 |
| **iOS: Channel Discovery & Home Hub** | `ChannelListView.swift`, `ChannelRow.swift`, `HomeHubView.swift`, `QuickRailsView.swift`, `SmartSearchResultsView.swift` | 38 | 0 | **Verified** | Batch 2 |
| **iOS: Guide & EPG Grid** | `GuideView.swift`, `GuideGrid.swift`, `GuideComponents.swift`, `TimeFilterPill.swift` | 16 | 0 | **Verified** | Batch 3 |
| **iOS: Recordings, Timers & Details** | `RecordingsView.swift`, `TimersView.swift`, `ProgramDetailSheet.swift` | 44 | 0 | **Verified** | Batch 4 |
| **iOS: Player Overlays & Debug HUD** | `PlayerScreen.swift`, `RecordingPlayerScreen.swift`, `TestTSPlayerScreen.swift`, `OfflinePlayerScreen.swift` | 28 | 0 | **Verified** | Batch 5 |
| **WebUI: Core Player & Shell** | `AppShell.tsx`, `Dashboard.tsx`, `Settings.tsx`, `EPG.tsx`, `PlayerControls.tsx` | 145 | 0 | **Verified** | Package 1 |
| **WebUI: Admin & Security Console** | `AdminLayout.tsx`, `DevicesManagementSection.tsx`, `SecuritySettingsSection.tsx`, `ProfileManagementSection.tsx`, `ConcurrencySettingsSection.tsx`, `ParentalControlSection.tsx`, `FamilyManagementSection.tsx`, `AccessTimesSection.tsx`, `AuditNotificationsSection.tsx` | 236 | 0 | **Verified** | Batch 6 |

**Total Verified Application Strings:** 636 strings (255 iOS/tvOS + 381 WebUI)  
**Total Outstanding Strings:** 0 strings  
**Verified Completion:** 100% of application-owned surfaces across all 6 agreed batches.

---

## 2. Final Audit & Literal Classification

A systematic audit across both repositories classifies all remaining string literals into three distinct categories:

### Category A: Application UI Text (100% Migrated & Verified)
All user-facing application controls, navigation bars, buttons, placeholders, dialogs, error messages, toast notifications, status badges, accessibility labels, and time/date formatters are completely externalized to native localization catalogs:
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

## 3. WebUI Batch 6 Detailed Breakdown & Evidence

### Migrated Admin Components (Batch 6)
1. **`AdminLayout.tsx` & Inline Panels (24 strings):** Sidebar navigation labels and descriptions for all 10 management sections, header titles, inline account data panel, and recordings quota panel.
2. **`DevicesManagementSection.tsx` (14 strings):** DPoP trusted device list, 30-day trust badges, device type prefixes, empty states, and revocations.
3. **`SecuritySettingsSection.tsx` (38 strings):** WebAuthn passkey management, admin token login, session revocation modals, registration forms, and error states.
4. **`ProfileManagementSection.tsx` (36 strings):** Viewing profile creation, editing, FSK maximum rating thresholds, PIN configuration, unknown rating policy options, and deletion confirmation dialogs.
5. **`ConcurrencySettingsSection.tsx` (22 strings):** Hardware tuner limits, active client viewer streams, protected DVR workers, FFmpeg transcoder slots, and deterministic preemption priority ranks.
6. **`ParentalControlSection.tsx` (24 strings):** Real-time child approval requests, single-view vs. permanent approval actions, denial feedback, and past decision history.
7. **`FamilyManagementSection.tsx` (28 strings):** Household members list, RBAC role badges, single-use invite code generator modal, and clipboard feedback.
8. **`AccessTimesSection.tsx` (26 strings):** Weekday bitmask selector (`Mon`–`Sun`), 24-hour visual viewing window slider, product permissions, and fail-closed arbitration settings.
9. **`AuditNotificationsSection.tsx` (24 strings):** SHA-256 cryptographic immutability integrity badge, browser WebPush activation, filter controls, and tabular audit log headers.

### Verification Evidence
- **TypeScript Typecheck:** `npm run type-check` (`tsc --noEmit`) -> **0 errors**.
- **Unit / Contract Tests:** `npx vitest run src/components/admin/AdminLayout.test.tsx tests/passkey-auth.journey.test.tsx tests/contracts/i18n.type-contract.test.ts` -> **3 test files, 9 tests passing**.
- **Rendered Bilingual Assertions:** `AdminLayout.test.tsx` confirms that all 10 reachable sections switch and render translated inner content in both English and German directly from `en.json` and `de.json`.
- **Full WebUI Suite:** `vitest run` -> **154/154 test files passed, 863/863 tests passed**.
- **Production Build:** `npm run build` (`tsc --noEmit && vite build`) -> **Success (745ms)**.
