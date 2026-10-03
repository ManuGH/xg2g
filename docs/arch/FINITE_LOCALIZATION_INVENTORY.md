# Finite Localization Inventory: iOS, tvOS & WebUI Surfaces

- **Status:** Active Roadmap & Surface Audit
- **Branch:** `feat/ios-string-catalog-foundation`
- **Base Verification:** Commit `7aff50ca` (20 Playback & Localization Tests Passing; tvOS Scheme Passing)
- **Locale Scope:** English (EN, Development Region) & German (DE, Target Locale)
- **Preservation Contract:** Enigma2 / OpenWebIF external media broadcast data (EPG programme titles, event summaries, channel broadcast names, stream genres) are strictly preserved **verbatim** in their broadcast language. Only application-owned chrome, navigation, buttons, accessibility labels, statuses, and format strings are localized.

---

## 1. Surface Overview & Progress Summary

| Surface Domain | Screens / Components | Total Strings | Migrated | Outstanding | Status | Batch |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **iOS: Settings & Infrastructure** | `SettingsView.swift`, `Theme.swift`, `DeviceCapabilities.swift` | 65 | 65 | 0 | **Verified** | Package 2 |
| **iOS: Error Presentation & Diagnostics** | `UserFacingError.swift`, `APIError.swift`, `ProblemDetails.swift` | 42 | 42 | 0 | **Verified** | Package 3 |
| **iOS: Shell, Navigation & Pairing** | `RootView.swift`, `MiniPlayerBar.swift`, `PadRootView.swift`, `TVRootView.swift` | 22 | 22 | 0 | **Verified** | Batch 1 |
| **iOS: Channel Discovery & Home Hub** | `ChannelListView.swift`, `ChannelRow.swift`, `HomeHubView.swift`, `QuickRailsView.swift`, `SmartSearchResultsView.swift` | 38 | 0 | 38 | **Pending** | Batch 2 |
| **iOS: Guide & EPG Grid** | `GuideView.swift`, `GuideGrid.swift`, `GuideComponents.swift`, `TimeFilterPill.swift` | 16 | 0 | 16 | **Pending** | Batch 3 |
| **iOS: Recordings, Timers & Details** | `RecordingsView.swift`, `TimersView.swift`, `ProgramDetailSheet.swift` | 44 | 0 | 44 | **Pending** | Batch 4 |
| **iOS: Player Overlays & Debug HUD** | `PlayerScreen.swift`, `RecordingPlayerScreen.swift`, `TestTSPlayerScreen.swift` | 28 | 4 | 24 | **Pending** | Batch 5 |
| **WebUI: Core Player & Shell** | `AppShell.tsx`, `Dashboard.tsx`, `Settings.tsx`, `EPG.tsx`, `PlayerControls.tsx` | 145 | 145 | 0 | **Verified** (Pkg 1) | Package 1 |
| **WebUI: Admin & Security Console** | `DevicesManagementSection.tsx`, `PasskeyAuthFlow.tsx`, `AdminLayout.tsx`, `ProfileManagementSection.tsx` | 32 | 0 | 32 | **Pending** | Batch 6 |

**Total Application Strings:** ~432 strings  
**Currently Migrated & Tested:** 278 strings (64.4%)  
**Remaining Across All Batches:** 154 strings (35.6%)

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
- **Estimated Effort:** 2.0 hours
- **Screens:** `ChannelListView.swift`, `ChannelRow.swift`, `HomeHubView.swift`, `QuickRailsView.swift`, `SmartSearchResultsView.swift`
- **Strings to Migrate:**
  - Section headers: `Für dich` / `For You`, `Prime Time` / `Prime Time`, `HEUTE 20:15 UHR` / `TODAY 8:15 PM`, `MEINE FAVORITEN` / `MY FAVORITES`, `ZULETZT GESPIELT` / `RECENTLY PLAYED`, `Weiterschauen` / `Continue Watching`.
  - Filter pills: `Jetzt Live` / `Live Now`, `20:15` / `8:15 PM`, `22:00` / `10:00 PM`.
  - Row labels: `DANACH:` / `NEXT:`, `Keine Programminformationen verfügbar` / `No program information available`, `• %lld Min` / `• %lld min`, `• noch %lld Min` / `• %lld min left`.
  - Actions: `Aufnehmen` / `Record`, `Live ansehen` / `Watch Live`, `Alle anzeigen` / `Show All`.
  - Empty search: `Weder Sender noch laufende oder kommende Sendungen entsprechen deiner Suche.` / `No channels or programs match your search.`
- **EPG Preservation Rule:** Show title `show.title` and broadcast description `show.shortDescription` are wrapped in `Text(verbatim:)` to prevent translation lookup collisions.

---

### Surface 3: Guide & EPG Grid — Batch 3
- **Estimated Effort:** 1.5 hours
- **Screens:** `GuideView.swift`, `GuideGrid.swift`, `GuideComponents.swift`, `TimeFilterPill.swift`
- **Strings to Migrate:**
  - Filters: `Alle Sender` / `All Channels`, `Favoriten (%lld)` / `Favorites (%lld)`.
  - Quick time jump buttons: `Jetzt` / `Now`, `20:15` / `8:15 PM`, `22:00` / `10:00 PM`.
  - Action labels: `%@ abspielen` / `Play %@`, `„%@“ aufnehmen` / `Record “%@”`.
  - Accessibility: Full VoiceOver composite format string for program cells.

---

### Surface 4: Recordings, Timers & Details — Batch 4
- **Estimated Effort:** 2.5 hours
- **Screens:** `RecordingsView.swift`, `TimersView.swift`, `ProgramDetailSheet.swift`
- **Strings to Migrate:**
  - Recordings: `Aufnahmen` / `Recordings`, `Aufnahmedetails` / `Recording Details`, `Aufnahme abspielen` / `Play Recording`, `Von Beginn an abspielen` / `Play from Start`, `Fortsetzen bei %@` / `Resume at %@`, `INHALTSANGABE` / `SUMMARY`, `METADATEN` / `METADATA`, `Dateiname` / `Filename`, `Service-Ref` / `Service Ref`, `Download-Qualität für Offline:` / `Offline Download Quality:`, `Vom Server löschen` / `Delete from Server`, `„%@“ wird unwiderruflich von der Festplatte gelöscht.` / `“%@” will be permanently deleted from the disk.`, `Schließen` / `Close`.
  - Timers: `Timer` / `Timers`, `Neuer Timer` / `New Timer`, `Derzeit sind keine Aufnahme-Timer auf der Vu+ Uno 4K geplant.` / `No recording timers are currently scheduled on the receiver.`, `NIMMT AUF` / `RECORDING`, `Sendungsdaten` / `Broadcast Info`, `Sender` / `Channel`, `Sender wählen…` / `Select channel…`, `Sendezeit` / `Broadcast Time`, `Start: %@` / `Start: %@`, `Ende: %@` / `End: %@`, `Planen` / `Schedule`, `Abbrechen` / `Cancel`.
  - Program Detail Sheet: `Sendungsdetails` / `Program Details`, `LÄUFT JETZT LIVE` / `LIVE NOW`, `Davor` / `Earlier`, `Danach` / `Later`, `Diese Folge aufnehmen` / `Record This Episode`, `Folge programmiert` / `Episode Scheduled`, `Timer aufnehmen` / `Add Timer`, `Timer programmiert` / `Timer Scheduled`, `WEITERE SENDETERMINE & FOLGEN (%lld)` / `MORE BROADCASTS & EPISODES (%lld)`.

---

### Surface 5: Player Overlays & Debug HUD — Batch 5
- **Estimated Effort:** 1.5 hours
- **Screens:** `PlayerScreen.swift`, `RecordingPlayerScreen.swift`, `TestTSPlayerScreen.swift`
- **Strings to Migrate:**
  - Drawer & Channels: `SCHNELL-ZAPPING` / `QUICK ZAPPING`, `SENDER & LIVE-PROGRAMM` / `CHANNELS & LIVE PROGRAM`, `LIVE • DVR` / `LIVE • DVR`, `Von Beginn` / `From Start`, `Zur Live-Kante` / `Jump to Live Edge`.
  - Native HUD / Status: `WÄRMT…` / `WARMING…`, `Laden` / `Loading`, `AKTIV` / `ACTIVE`, `Aus` / `Off`, `TIMESHIFT` / `TIMESHIFT`, `Timeshift wird vorbereitet…` / `Preparing timeshift…`, `Wird vorbereitet…` / `Preparing…`.
  - Incompatibility HUD: `Der Sender überträgt %@. Dieses Gerät kann das bei Direktwiedergabe nicht dekodieren.` / `The channel broadcasts %@. This device cannot decode this with direct playback.`, `Stelle unter Einstellungen → Wiedergabe-Art auf „Über den Server“ um, dann läuft dieser Sender.` / `Switch Playback Mode to “Server Streaming” in Settings to play this channel.`.

---

### Surface 6: WebUI Admin & Security Console — Batch 6
- **Estimated Effort:** 2.0 hours
- **Screens:** `DevicesManagementSection.tsx`, `PasskeyAuthFlow.tsx`, `AdminLayout.tsx`, `ProfileManagementSection.tsx`, `ParentalControlSection.tsx`, `ConcurrencySettingsSection.tsx`
- **Strings to Migrate:**
  - Admin Navigation, Device authorization/revoke actions, WebAuthn Passkey prompts, Concurrency limits, Parental PIN dialogs.

---

## 3. Implementation Sequence & Gates

1. **Commit Inventory:** Lock this document into `docs/arch/FINITE_LOCALIZATION_INVENTORY.md`.
2. **Execute Batch 1:** Shell, Navigation & Pairing (`RootView.swift`, `MiniPlayerBar.swift`).
   - Add catalog entries to `Localizable.xcstrings` (DE & EN).
   - Update `RootView.swift` and `MiniPlayerBar.swift` with `LocalizedStringResource` / localized strings.
   - Run regression test suite (20 tests) + tvOS build.
   - Render screenshot evidence in EN & DE.
3. **Execute Batch 2:** Channel Discovery & Home Hub.
4. **Execute Batch 3:** Guide & EPG Grid.
5. **Execute Batch 4:** Recordings, Timers & Details.
6. **Execute Batch 5:** Player Overlays & Debug HUD.
7. **Execute Batch 6:** WebUI Admin & Security Console.
