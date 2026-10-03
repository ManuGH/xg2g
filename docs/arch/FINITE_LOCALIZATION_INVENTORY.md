# Finite Localization Inventory: iOS, tvOS & WebUI Surfaces

- **Status:** Batches 1–5 Completed & Verified on iOS/tvOS; Batch 6 In Progress on WebUI
- **Branches:** `feat/ios-string-catalog-foundation` (iOS/tvOS) & `feat/webui-translation-key-safety` (WebUI)
- **Base Verification:** 26/26 iOS Playback & Localization Tests Passing; tvOS Scheme Build Succeeded; WebUI strict key checks contract passing.
- **Locale Scope:** English (EN, Development Region) & German (DE, Target Locale)
- **Preservation Contract:** Enigma2 / OpenWebIF external media broadcast data (EPG programme titles, event summaries, channel broadcast names, stream genres, and receiver bouquet names) are strictly preserved **verbatim** in their broadcast language using `Text(verbatim:)`. Only application-owned chrome, navigation, buttons, accessibility labels, statuses, and format strings are localized.

---

## 1. Surface Overview & Progress Summary

| Surface Domain | Screens / Components | Total Strings | Migrated | Outstanding | Status | Batch |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **iOS: Settings & Infrastructure** | `SettingsView.swift`, `Theme.swift`, `DeviceCapabilities.swift` | 65 | 65 | 0 | **Verified** | Package 2 |
| **iOS: Error Presentation & Diagnostics** | `UserFacingError.swift`, `APIError.swift`, `ProblemDetails.swift` | 42 | 42 | 0 | **Verified** | Package 3 |
| **iOS: Shell, Navigation & Pairing** | `RootView.swift`, `MiniPlayerBar.swift`, `PadRootView.swift`, `TVRootView.swift` | 22 | 22 | 0 | **Verified** | Batch 1 |
| **iOS: Channel Discovery & Home Hub** | `ChannelListView.swift`, `ChannelRow.swift`, `HomeHubView.swift`, `QuickRailsView.swift`, `SmartSearchResultsView.swift` | 38 | 38 | 0 | **Verified** | Batch 2 |
| **iOS: Guide & EPG Grid** | `GuideView.swift`, `GuideGrid.swift`, `GuideComponents.swift`, `TimeFilterPill.swift` | 16 | 16 | 0 | **Verified** | Batch 3 |
| **iOS: Recordings, Timers & Details** | `RecordingsView.swift`, `TimersView.swift`, `ProgramDetailSheet.swift` | 44 | 44 | 0 | **Verified** | Batch 4 |
| **iOS: Player Overlays & Debug HUD** | `PlayerScreen.swift`, `RecordingPlayerScreen.swift`, `TestTSPlayerScreen.swift`, `OfflinePlayerScreen.swift` | 28 | 28 | 0 | **Verified** | Batch 5 |
| **WebUI: Core Player & Shell** | `AppShell.tsx`, `Dashboard.tsx`, `Settings.tsx`, `EPG.tsx`, `PlayerControls.tsx` | 145 | 145 | 0 | **Verified** | Package 1 |
| **WebUI: Admin & Security Console** | `DevicesManagementSection.tsx`, `PasskeyAuthFlow.tsx`, `AdminLayout.tsx`, `ProfileManagementSection.tsx`, `ConcurrencySettingsSection.tsx`, `ParentalControlSection.tsx`, `FamilyManagementSection.tsx`, `AccessTimesSection.tsx`, `AuditNotificationsSection.tsx` | 80 | 32 | 48 | **In Progress** | Batch 6 |

**Total Estimated Application Strings:** ~480 strings  
**Currently Migrated & Tested:** ~432 strings (~90%)  
**Outstanding (Batch 6 in progress):** ~48 strings (~10%)

---

## 2. Detailed Surface Breakdown (Outstanding Surfaces)

### Surface 1: iOS Shell, Navigation & Pairing (`RootView.swift`, `MiniPlayerBar.swift`) — Batch 1
- **Estimated Effort:** 1.5 hours
- **Verification Status:** **Verified (Pass)**: 23 regression/localization tests passing; tvOS build passing; `server_setup_en.png`, `server_setup_de.png`, `pairing_en.png`, `pairing_de.png` visual rendering confirmed.
- **Key Strings Migrated:**
  1. Tab item: Channels (`Sender` / `Channels`)
  2. Tab item: Guide (`Programm` / `Guide`)
  3. Tab item: Recordings (`Aufnahmen` / `Recordings`)
  4. Tab item: Timers (`Timer` / `Timers`)
  5. Tab item: Settings (`Einstellungen` / `Settings`)
  6. Pairing Title: `Geräte-Kopplung` / `Device Pairing`
  7. Pairing Subtitle: `Dieses Gerät benötigt eine einmalige Genehmigung, bevor Streams gestartet werden können.` / `This device requires one-time approval before streams can be played.`
  8. Key Gen Status: `Generiere P-256 Hardwareschlüssel & starte Kopplung…` / `Generating P-256 hardware key & starting pairing…`
  9. Connect Form Title: `Mit xg2g verbinden` / `Connect to xg2g`
  10. Connect Form Description: `Gib die Adresse deines xg2g-Servers ein.` / `Enter the address of your xg2g server.`
  11. Connect Button: `Start Pairing` / `Start Pairing`
  12. Pairing Prompt: `Gib diesen Code in deiner Web-Admin-Konsole unter Geräte ein:` / `Enter this code in your Web Admin console under Devices:`
  13. Approval Wait: `Warte auf Bestätigung in der Admin-Konsole…` / `Waiting for approval in admin console…`
  14. Code Expiry Format: `Code gültig noch %@` / `Code valid for %@`
  15. Bouquet Header: `Bouquets & Sendergruppen` / `Bouquets & Channel Groups`
  16. All Channels Bouquet: `Alle Sender` / `All Channels`
  17. Favorites Bouquet: `Favoriten` / `Favorites`
  18. Footer Badge: `xg2g Broadcast System • 2026` / `xg2g Broadcast System • 2026`
  19. App Title: `xg2g TV` / `xg2g TV`
  20. MiniPlayer Recording Badge: `AUFNAHME` / `REC`
  21. Channel Count Format: `%lld Sender` / `%lld channels`
  22. Accessibility Label: `Aktuelle Wiedergabe öffnen` / `Open current playback`

---

### Surface 2: Channel Discovery & Home Hub — Batch 2
- **Verification Status:** **Verified (Pass)**: Commit `8a839f57`. 22 regression and localization tests passing; tvOS build passing; `channel_list_de.png`, `channel_list_en.png`, `home_hub_de.png`, `home_hub_en.png` visual rendering confirmed.
- **Screens:** `ChannelListView.swift`, `ChannelRow.swift`, `HomeHubView.swift`, `QuickRailsView.swift`, `SmartSearchResultsView.swift`
- **EPG Preservation Rule:** Show title `show.title` and broadcast description `show.shortDescription` are wrapped in `Text(verbatim:)` to prevent translation lookup collisions. Synthetic favorites bouquet localized by stable ID `AppModel.favoritesBouquetID`.

---

### Surface 3: Guide & EPG Grid — Batch 3
- **Verification Status:** **Verified (Pass)**: Commit `f2a38785`. 23 regression and localization tests passing; tvOS build passing; `guide_de.png`, `guide_en.png` visual rendering confirmed.
- **Screens:** `GuideView.swift`, `GuideGrid.swift`, `GuideComponents.swift`, `TimeFilterPill.swift`
- **Strings Migrated:** Filter pills, quick time jumps (`Now`, `8:15 PM`, `10:00 PM`), action context menus (`Play %@`, `Record “%@”`), and locale-aware formatters.

---

### Surface 4: Recordings, Timers & Details — Batch 4
- **Verification Status:** **Verified (Pass)**: Commit `f6c82b2f`. 24 regression and localization tests passing; tvOS build passing; `recordings_de.png`, `recordings_en.png`, `timers_de.png`, `timers_en.png` visual rendering confirmed.
- **Screens:** `RecordingsView.swift`, `TimersView.swift`, `ProgramDetailSheet.swift`
- **Strings Migrated:** Recording detail cards, server deletion dialogs, timer creation forms, schedule pickers, episode detail sheets, and rerun lists with verbatim broadcast title protection.

---

### Surface 5: Player Overlays & Debug HUD — Batch 5
- **Verification Status:** **Verified (Pass)**: Commit `c4ea3a48`. 26 regression and localization tests passing; tvOS build passing; `offline_player_de.png`, `offline_player_en.png` visual rendering confirmed.
- **Screens:** `PlayerScreen.swift`, `RecordingPlayerScreen.swift`, `TestTSPlayerScreen.swift`, `OfflinePlayerScreen.swift`
- **Strings Migrated:** Player quick-zapping drawers, native telemetry HUD sections, aspect ratio presets (`localizedShortLabel`), timeshift timeline bar and jump to live-edge controls, unplayable format guidance notices, and offline recording players.

---

### Surface 6: WebUI Admin & Security Console — Batch 6
- **Verification Status:** **Verified (Pass)**: Commit `bd33bf0c` in `xg2g-webui-i18n-keys`. TypeScript type check (`tsc --noEmit`), Vitest unit/contract tests, and production build (`npm run build`) succeeded cleanly.
- **Screens:** `DevicesManagementSection.tsx`, `PasskeyAuthFlow.tsx`, `AdminLayout.tsx`, `ProfileManagementSection.tsx`, `ConcurrencySettingsSection.tsx`, `ParentalControlSection.tsx`
- **Strings Migrated:** Admin Material 3 navigation and headers, DPoP trusted device list and revocations, WebAuthn passkey registration/login/recovery bootstrap flows, viewing profile controls, and tuner concurrency limit forms under strict key type safety (`strictKeyChecks: true`).
