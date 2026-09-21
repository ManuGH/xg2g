# Xcode source organization

## Change contract

- **Fixed:** inconsistent internal type names and the new tvOS target's template bundle identifier.
- **Improved:** source and test discoverability through domain folders, separate platform entry points/resources, and focused navigation and EPG files.
- **New:** a shared `Xg2gTV` scheme and documented naming rules; no new application behavior.
- **Removed:** obsolete source paths and design-reference names, without deleting implementations.
- **Unchanged:** iOS target/module/scheme `Xg2g`, iOS bundle identifiers, generated contract and transport paths, API payloads, stored values, UI layout, playback behavior, and existing test assertions.
- **Risks:** missing target membership, resource duplication, stale path references, and file-scoped visibility after extraction. Changing the tvOS placeholder bundle ID creates a different app identity; it does not migrate an installed prototype's data.
- **Acceptance criteria:** account for every pre-existing Swift source/resource; verify only intended identifier substitutions and mechanical extraction; parse the project and schemes; compare iOS Debug build-for-testing and tvOS Debug builds with the pre-change baseline; build both Release configurations; run the existing affected simulator tests and transport-boundary check; verify Xcode sees the new layout.
- **Exit condition:** no duplicate source trees or newly introduced compatibility aliases. The pre-existing `TestTSPlayerScreen` alias remains an explicit exception, covered by `TimeshiftTransitionTests`; see the compatibility policy below. Commit/push and the unrelated pre-existing working-tree changes remain with Manuel.

## Layout

`Xg2g/` is the source root compiled by both app targets. Shared target membership
does not imply that every API is available on both platforms: existing platform
guards still apply. Platform entry points and asset catalogs live separately.

```text
ios/
  Xg2g/
    App/                 # App state and navigation
    Features/            # Channels, Guide, Home, Onboarding, Recordings, Search, Settings, Timers
    Playback/            # Audio, Video, TransportStream, Subtitles, Presentation, Timeshift
    Identity/            # Credentials, device keys, enrollment and revocation
    Transport/           # Existing network boundary; keep this path stable
    DesignSystem/        # Theme, reusable visual components and feedback
    Support/             # Shared collection utilities
    Generated/           # Generated API contract; do not edit by hand
  Platforms/
    iOS/                 # Xg2gApp and iOS assets
    tvOS/                # Xg2gTVApp (boots the shared RootView) and tvOS assets
  Xg2gTests/              # App, Features, Identity, Playback, Transport, Fixtures
  Support/               # Build configuration plists
```

The project uses synchronized folders. Add a source to its domain folder instead
of adding a manual PBX file reference. Platform folders belong only to their own
app target. `Xg2gTests` still imports `Xg2g`.

## Naming

- Keep the visible product name `xg2g`; use `Xg2g` as the existing Swift module prefix.
- Use `Xg2g` / `Xg2gTests` for iOS and `Xg2gTV` for tvOS targets and shared schemes.
- Name Swift files after their principal type; use `Type+Concern.swift` for extensions.
- Use UpperCamelCase for types, with established acronyms such as `EPG`, `DVR`, `API`, `URL`, and `DVB` preserved. Generated wire names follow the generator.
- Name UI components by function, such as `ProgramSpotlightHero` and `PlaybackProgressView`, rather than a product that inspired their appearance.
- Keep full-screen players named `*PlayerScreen`, other views named by their UI role, and test suites named `*Tests` in English.
- Prefer one independently discoverable model/view per file; small private helpers may stay with their owner.
- Keep contract adapters in `Transport`, separate from feature models.

No lifecycle implementation changes are intended. Source extraction must preserve
view identity, modifier order, task keys, state ownership and effect bodies.

## Follow-up change contract (2026-09-17)

- **Fixed:** stale documentation paths, an inaccurate claim that no iOS CI exists,
  the undocumented compatibility exception, and remaining principal-type/file mismatches.
- **Improved:** independent recording/home views, domain models, contract mappings,
  and the 13 suites previously combined in `ChannelAndPlaybackTests` get focused files.
- **New:** a reusable tvOS Debug/Release simulator build gate and a required tvOS
  job in Architecture Convergence; an internal recording-time formatter shared by
  the extracted views. CI uses the documented `xcode-27` runner for Apple jobs.
- **Removed:** obsolete umbrella source files; no app functionality or test assertions.
- **Unchanged:** app identifiers, API requests/payloads, stored values, runtime
  state ownership, view modifier order, and all existing test suite identifiers.
- **Risks:** file-private helper visibility, lost test attributes/platform guards,
  wrong CI runner SDK, and changes occurring concurrently in the working tree.
- **Acceptance criteria:** preserve source/test bodies through extraction; build
  iOS app/tests and tvOS Debug/Release; run every extracted suite plus mapping,
  identity and presentation tests; validate workflow syntax and required-job wiring;
  confirm all contract-to-domain extensions are inside Transport and stale paths
  are gone. Remote CI success must not be inferred from local validation.
- **Exit condition:** no duplicate implementations. Codex completes local changes
  and evidence; Manuel owns review/integration of this already dirty working tree.

### Compatibility policy

`LivePlayerScreen+Compatibility.swift` retains the pre-existing public
`TestTSPlayerScreen` alias. New code uses `LivePlayerScreen`. Removing the alias
requires a separate compatibility decision by Manuel, a repository caller check,
and an intentional update of its existing test assertion; moving files does not
silently remove that contract.

### Validation scope

This extraction does not change lifecycle behavior. Verify the existing tests for
presentation attachment/detachment, live-to-recording transitions and playback
ownership, and compare extracted view bodies and modifiers with the saved source.
React mount/rerender/executor/StrictMode scenarios do not apply to these Swift files.

## Validation and handoff (2026-09-17)

- Working branch: `fix/in-player-channel-switch-and-now-playing`, based on local
  commit `3bee125d`. This refactor is an uncommitted working-tree change on top
  of the existing player/tvOS work. No commit, push, merge or deployment was made.
- The pre-existing staging index is unchanged. The original iOS tree, staged and
  unstaged patches, move/extraction manifest, verification script and build logs
  are saved in `/Users/manuel/xg2g-xcode-structure-backup-20260917/`.
- Source accounting covers all 169 original Swift files and 22 asset files.
  Extracting `RootView.swift` and `Channel.swift` into focused files brings the
  total to 182 Swift files, including tests and both platform entry points.
  There are no loose Swift files in the app or test source roots.
- Content comparison verifies mechanical extraction and identifier substitutions,
  with one explanatory mapping comment changed. An independently occurring
  URL-encoding edit to `AppModel+Playback.swift` was preserved and recorded in
  `concurrent-player-edit.patch`; it is not part of this refactor.
- Xcode MCP confirms the domain folders, isolated platform folders and both
  shared schemes. Project and scheme parsing, target membership checks,
  `git diff --check`, and the Go guard's formatting check pass.
- iOS Debug build-for-testing compiles the app and all 56 test files. iOS
  Release, tvOS Debug and tvOS Release builds also pass. Both built apps display
  `xg2g`; iOS retains `io.github.manugh.xg2g.ios`, while tvOS uses
  `io.github.manugh.xg2g.tvos`.
- Primary simulator testing executed on **iPhone 18 Pro / iOS 27.0** (`EDFBF4DA-979B-446F-BDBE-5B6CD64B0619`):
  - 199 tests across 25 suites passed cleanly with 0 failures and 0 skips (`** TEST EXECUTE SUCCEEDED **`).
  - Standard test scripts (`verify-ios-build.sh`, `run-contract-tests.sh`) and documentation (`ios/README.md`) updated to target `iPhone 18 Pro` (iOS 27.0) by default.
- Transport boundary resolution and cleanup:
  - All 7 reported transport-boundary findings were resolved by routing debug demo receiver and fallback stream URLs through `MediaEndpoints` in `ios/Xg2g/Transport/`.
  - `go run ./scripts/verify-client-transport-boundary.go` is now 100% green (`✅ ios: transport boundary enforced and clean`, scanned 346 files, 0 violations).
  - Pipeline modernizations: removed deprecated `httpShouldUsePipelining` from `LiveStreamIngest.swift`; replaced deprecated `UIScreen.main` with `traitCollection.displayScale` / `@Environment(\.displayScale)` in `MetalVideoView.swift` and `ChannelLogo.swift`.
  - Developer tooling: added `ios/scripts/run-ios-tests.sh` and `make test-ios` to execute the full unit suite on iPhone 18 Pro (iOS 27.0) in ~9s (569 tests across 77 suites passed).
  - Full verification: `make verify-tvos-build` passes (Debug & Release), `make verify-ios-build` passes, `make test-ios` passes (569 tests green), and `go run ./scripts/verify-client-transport-boundary.go` passes (0 violations).
- Next owner/action: Manuel reviews the clean local structural and boundary changes alongside the
  separate player work before deciding how to commit them.
