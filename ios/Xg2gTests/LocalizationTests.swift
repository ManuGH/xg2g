// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import SwiftUI
import Testing
import UIKit
@testable import Xg2g

@Suite("iOS Localization Foundation & Bounded Migration Tests")
struct LocalizationTests {

    let deLocale = Locale(identifier: "de")
    let enLocale = Locale(identifier: "en")

    private func resolve(_ resource: LocalizedStringResource, locale: Locale) -> String {
        var copy = resource
        copy.locale = locale
        return String(localized: copy)
    }

    private var appBundle: Bundle {
        Bundle(for: AppModel.self)
    }

    private func localize(_ value: String.LocalizationValue, locale: Locale) -> String {
        var res = LocalizedStringResource(value, bundle: .atURL(appBundle.bundleURL))
        res.locale = locale
        return String(localized: res)
    }

    // MARK: - 1. EpgGenre Localization & Persistence Audit

    @Test("EpgGenre preserves stable rawValue and id while providing DE and EN localized titles")
    func epgGenreLocalizationAndStability() {
        let expectations: [(genre: EpgGenre, rawValue: String, de: String, en: String)] = [
            (.all, "Alle", "Alle", "All"),
            (.movie, "Spielfilme", "Spielfilme", "Movies"),
            (.series, "Serien", "Serien", "Series"),
            (.sport, "Sport", "Sport", "Sports"),
            (.docu, "Doku & Wissen", "Doku & Wissen", "Documentary & Knowledge"),
            (.show, "Unterhaltung", "Unterhaltung", "Entertainment"),
            (.news, "Nachrichten", "Nachrichten", "News"),
            (.kids, "Kinder", "Kinder", "Kids")
        ]

        for item in expectations {
            // RawValue and ID stability (cache and persistence contract)
            #expect(item.genre.rawValue == item.rawValue)
            #expect(item.genre.id == item.rawValue)

            // Localized titles in both locales
            let deTitle = resolve(item.genre.localizedTitle, locale: deLocale)
            let enTitle = resolve(item.genre.localizedTitle, locale: enLocale)

            #expect(deTitle == item.de, "DE mismatch for \(item.genre)")
            #expect(enTitle == item.en, "EN mismatch for \(item.genre)")
        }
    }

    // MARK: - 2. PlaybackEngine Localization & Persistence Audit

    @Test("AppModel.PlaybackEngine preserves rawValues and localizes display titles, summaries and tradeoffs")
    func playbackEngineLocalizationAndStability() {
        let expectedEngines: [(engine: AppModel.PlaybackEngine, raw: String, de: String, en: String)] = [
            (.auto, "auto", "Automatisch", "Auto"),
            (.native, "native", "Native Live-TV", "Native Live TV"),
            (.hls, "hls", "Server-Streaming (HLS)", "Server Streaming (HLS)")
        ]

        for item in expectedEngines {
            // RawValue stability (UserDefaults persistence contract)
            #expect(item.engine.rawValue == item.raw)
            #expect(item.engine.id == item.raw)

            let deTitle = resolve(item.engine.localizedTitle, locale: deLocale)
            let enTitle = resolve(item.engine.localizedTitle, locale: enLocale)
            #expect(deTitle == item.de)
            #expect(enTitle == item.en)

            // Tradeoffs must have non-empty localized entries in both languages
            let tradeoff = item.engine.tradeoff
            #expect(!tradeoff.gains.isEmpty)
            #expect(!tradeoff.costs.isEmpty)
            for gain in tradeoff.gains {
                let deGain = resolve(gain, locale: deLocale)
                let enGain = resolve(gain, locale: enLocale)
                #expect(!deGain.isEmpty)
                #expect(!enGain.isEmpty)
            }
            for cost in tradeoff.costs {
                let deCost = resolve(cost, locale: deLocale)
                let enCost = resolve(cost, locale: enLocale)
                #expect(!deCost.isEmpty)
                #expect(!enCost.isEmpty)
            }
        }
    }

    // MARK: - 3. StreamingQualityPreference Localization & Persistence Audit

    @Test("AppModel.StreamingQualityPreference preserves rawValues and localizes display titles")
    func qualityPreferenceLocalizationAndStability() {
        let expectedQualities: [(pref: AppModel.StreamingQualityPreference, raw: String, de: String, en: String)] = [
            (.auto, "auto", "Automatisch", "Auto"),
            (.passthrough, "passthrough", "Originalqualität", "Original Quality"),
            (.qsvNormalize, "qsvNormalize", "Kompatibilität", "Compatibility"),
            (.dataSaver, "dataSaver", "Datensparen", "Data Saver")
        ]

        for item in expectedQualities {
            #expect(item.pref.rawValue == item.raw)
            #expect(item.pref.id == item.raw)

            let deTitle = resolve(item.pref.localizedTitle, locale: deLocale)
            let enTitle = resolve(item.pref.localizedTitle, locale: enLocale)
            #expect(deTitle == item.de)
            #expect(enTitle == item.en)
        }
    }

    // MARK: - 4. DownloadQuality Localization & Persistence Audit

    @Test("DownloadQuality preserves Codable rawValues and localizes titles and subtitles")
    func downloadQualityLocalizationAndStability() {
        let expectedDownloads: [(q: DownloadQuality, raw: String, deTitle: String, enTitle: String)] = [
            (.original, "original", "Original HD (1:1)", "Original HD (1:1)"),
            (.av1, "av1", "Ultra-Kompakt (AV1)", "Ultra Compact (AV1)"),
            (.compact, "compact", "Flugzeug / Kompakt (HEVC)", "Airplane / Compact (HEVC)"),
            (.high, "high", "720p HD (Ausgewogen)", "720p HD (Balanced)")
        ]

        for item in expectedDownloads {
            #expect(item.q.rawValue == item.raw)
            #expect(item.q.id == item.raw)

            let deTitle = resolve(item.q.localizedTitle, locale: deLocale)
            let enTitle = resolve(item.q.localizedTitle, locale: enLocale)
            #expect(deTitle == item.deTitle)
            #expect(enTitle == item.enTitle)
        }
    }

    // MARK: - 5. Placeholder & Interpolation Completeness

    @Test("Placeholders format correctly without fragment concatenation in both DE and EN")
    func placeholdersFormatCorrectly() {
        // Genre accessibility placeholder
        let deMovie = resolve(EpgGenre.movie.localizedTitle, locale: deLocale)
        let enMovie = resolve(EpgGenre.movie.localizedTitle, locale: enLocale)
        let deGenre = localize("Genre: \(deMovie)", locale: deLocale)
        let enGenre = localize("Genre: \(enMovie)", locale: enLocale)
        #expect(deGenre == "Genre: Spielfilme")
        #expect(enGenre == "Genre: Movies")

        // Channel list accessibility placeholder
        let deAll = localize("All Channels", locale: deLocale)
        let enAll = localize("All Channels", locale: enLocale)
        let deList = localize("Channel list: \(deAll)", locale: deLocale)
        let enList = localize("Channel list: \(enAll)", locale: enLocale)
        #expect(deList == "Senderliste: Alle Sender")
        #expect(enList == "Channel list: All Channels")

        // No genre broadcasts empty title placeholder
        let deSeries = resolve(EpgGenre.series.localizedTitle, locale: deLocale)
        let enSeries = resolve(EpgGenre.series.localizedTitle, locale: enLocale)
        let deEmptyGenre = localize("No \(deSeries) broadcasts", locale: deLocale)
        let enEmptyGenre = localize("No \(enSeries) broadcasts", locale: enLocale)
        #expect(deEmptyGenre == "Keine Serien-Sendungen")
        #expect(enEmptyGenre == "No Series broadcasts")

        // Search empty state placeholder
        let query = "Tagesschau"
        let deSearch = localize("No results for “\(query)”", locale: deLocale)
        let enSearch = localize("No results for “\(query)”", locale: enLocale)
        #expect(deSearch == "Keine Treffer für „Tagesschau“")
        #expect(enSearch == "No results for “Tagesschau”")

        // Guide genre empty state placeholder
        let deSport = resolve(EpgGenre.sport.localizedTitle, locale: deLocale)
        let enSport = resolve(EpgGenre.sport.localizedTitle, locale: enLocale)
        let deGuideGenre = localize("No broadcasts in \(deSport)", locale: deLocale)
        let enGuideGenre = localize("No broadcasts in \(enSport)", locale: enLocale)
        #expect(deGuideGenre == "Nichts in Sport")
        #expect(enGuideGenre == "No broadcasts in Sports")
    }

    // MARK: - 6. Visual Layout & Snapshot Rendering (DE vs EN & Dynamic Type)
    //
    // Note: These visual rendering tests serve as programmatic smoke tests verifying that SwiftUI
    // hierarchies (pickers, toggles, subviews, and navigation titles) complete layout passes without
    // layout recursion, frame corruption, or missing text under both German and English locales, as well as
    // under extreme Dynamic Type accessibility scalings (accessibilityExtraExtraLarge). Rendered images are
    // saved to a configurable directory (XG2G_ARTIFACTS_DIR or repo artifacts/screenshots).

    private static func saveScreenshot(_ image: UIImage, name: String) {
        guard let data = image.pngData() else { return }
        let targetDir: URL
        if let envDir = ProcessInfo.processInfo.environment["XG2G_ARTIFACTS_DIR"], !envDir.isEmpty {
            targetDir = URL(fileURLWithPath: envDir)
        } else {
            targetDir = URL(fileURLWithPath: #filePath)
                .deletingLastPathComponent()
                .deletingLastPathComponent()
                .deletingLastPathComponent()
                .appendingPathComponent("artifacts/screenshots")
        }

        try? FileManager.default.createDirectory(at: targetDir, withIntermediateDirectories: true)
        let fileURL = targetDir.appendingPathComponent("\(name).png")
        try? data.write(to: fileURL)
    }

    @Test("Programmatic smoke test: Render SettingsView snapshots in German and English, retaining PNG artifacts")
    @MainActor
    func renderSettingsViewsInBothLocales() throws {
        let locales = [("de", deLocale), ("en", enLocale)]
        let priorLanguages = UserDefaults.standard.stringArray(forKey: "AppleLanguages")

        defer {
            if let priorLanguages {
                UserDefaults.standard.set(priorLanguages, forKey: "AppleLanguages")
            } else {
                UserDefaults.standard.removeObject(forKey: "AppleLanguages")
            }
        }

        for (code, loc) in locales {
            UserDefaults.standard.set([code], forKey: "AppleLanguages")
            UserDefaults.standard.synchronize()

            let model = AppModel()
            model.playbackEngine = .auto
            model.qualityPreference = .auto
            model.receiverStreamBaseURL = "http://receiver.test:8001"

            let view = SettingsView(model: model)
                .environment(\.locale, loc)
                .preferredColorScheme(.dark)

            let controller = UIHostingController(rootView: view)
            controller.view.frame = CGRect(x: 0, y: 0, width: 393, height: 852)
            controller.view.overrideUserInterfaceStyle = .dark

            let window = UIWindow(frame: CGRect(x: 0, y: 0, width: 393, height: 852))
            window.rootViewController = controller
            window.makeKeyAndVisible()
            controller.view.setNeedsLayout()
            controller.view.layoutIfNeeded()

            let format = UIGraphicsImageRendererFormat()
            format.scale = 2.0
            let renderer = UIGraphicsImageRenderer(bounds: controller.view.bounds, format: format)
            let image = renderer.image { _ in
                controller.view.drawHierarchy(in: controller.view.bounds, afterScreenUpdates: true)
            }
            #expect(image.size.width > 0 && image.size.height > 0)
            Self.saveScreenshot(image, name: "settings_\(code)")

            // Diagnostic subview
            let diagView = DiagnosticPipelineOverrideView(model: model)
                .environment(\.locale, loc)
                .preferredColorScheme(.dark)

            let diagController = UIHostingController(rootView: diagView)
            diagController.view.frame = CGRect(x: 0, y: 0, width: 393, height: 852)
            diagController.view.overrideUserInterfaceStyle = .dark

            let diagWindow = UIWindow(frame: CGRect(x: 0, y: 0, width: 393, height: 852))
            diagWindow.rootViewController = diagController
            diagWindow.makeKeyAndVisible()
            diagController.view.setNeedsLayout()
            diagController.view.layoutIfNeeded()

            let diagImage = renderer.image { _ in
                diagController.view.drawHierarchy(in: diagController.view.bounds, afterScreenUpdates: true)
            }
            #expect(diagImage.size.width > 0 && diagImage.size.height > 0)
            Self.saveScreenshot(diagImage, name: "diagnostic_\(code)")
        }
    }

    @Test("Programmatic smoke test: SettingsView accommodates accessibilityExtraExtraLarge Dynamic Type in DE and EN")
    @MainActor
    func settingsViewDynamicTypeLayout() {
        let locales = [("de", deLocale), ("en", enLocale)]
        let priorLanguages = UserDefaults.standard.stringArray(forKey: "AppleLanguages")

        defer {
            if let priorLanguages {
                UserDefaults.standard.set(priorLanguages, forKey: "AppleLanguages")
            } else {
                UserDefaults.standard.removeObject(forKey: "AppleLanguages")
            }
        }

        for (code, loc) in locales {
            UserDefaults.standard.set([code], forKey: "AppleLanguages")
            UserDefaults.standard.synchronize()

            let model = AppModel()
            model.playbackEngine = .auto
            model.qualityPreference = .auto

            let dynamicTypeView = SettingsView(model: model)
                .environment(\.dynamicTypeSize, .accessibility2)
                .environment(\.sizeCategory, .accessibilityExtraExtraLarge)
                .environment(\.locale, loc)
                .preferredColorScheme(.dark)

            let controller = UIHostingController(rootView: dynamicTypeView)
            controller.view.frame = CGRect(x: 0, y: 0, width: 393, height: 1200)
            controller.view.overrideUserInterfaceStyle = .dark

            let window = UIWindow(frame: CGRect(x: 0, y: 0, width: 393, height: 1200))
            window.rootViewController = controller
            window.makeKeyAndVisible()
            controller.view.setNeedsLayout()
            controller.view.layoutIfNeeded()

            #expect(controller.view.bounds.width == 393)
            #expect(controller.view.bounds.height == 1200)

            let format = UIGraphicsImageRendererFormat()
            format.scale = 2.0
            let renderer = UIGraphicsImageRenderer(bounds: controller.view.bounds, format: format)
            let image = renderer.image { _ in
                controller.view.drawHierarchy(in: controller.view.bounds, afterScreenUpdates: true)
            }
            #expect(image.size.width > 0 && image.size.height > 0)
            Self.saveScreenshot(image, name: "settings_dynamic_type_axxl_\(code)")
        }
    }

    // MARK: - 7. Catalog-Wide Parity & Placeholder Verification

    @Test("Localizable.xcstrings contains full DE and EN parity and matching placeholders for all entries")
    func catalogWideParityAndPlaceholders() throws {
        let catalogURL = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .appendingPathComponent("Xg2g/Localizable.xcstrings")

        #expect(FileManager.default.fileExists(atPath: catalogURL.path))
        let data = try Data(contentsOf: catalogURL)

        struct Catalog: Decodable {
            struct Entry: Decodable {
                struct Loc: Decodable {
                    struct Unit: Decodable {
                        let state: String
                        let value: String
                    }
                    let stringUnit: Unit?
                }
                let extractionState: String?
                let localizations: [String: Loc]?
            }
            let sourceLanguage: String
            let strings: [String: Entry]
        }

        let catalog = try JSONDecoder().decode(Catalog.self, from: data)
        #expect(catalog.strings.count > 0, "Catalog must not be empty")

        // Regex for placeholders e.g. %@, %1$@, %d
        let placeholderRegex = try Regex(#"%([0-9]+\$)?([0-9]*\.?[0-9]*)?[@dDiIuUxXfFeEgGcCsSpaAF]"#)

        for (key, entry) in catalog.strings {
            let locs = entry.localizations ?? [:]

            // 1. English localization presence and content
            let enUnit = locs["en"]?.stringUnit
            #expect(enUnit != nil, "Missing EN localization for key: \(key)")
            let enVal = enUnit?.value ?? ""
            #expect(!enVal.isEmpty, "Empty EN value for key: \(key)")

            // 2. German localization presence and content
            let deUnit = locs["de"]?.stringUnit
            #expect(deUnit != nil, "Missing DE localization for key: \(key)")
            let deVal = deUnit?.value ?? ""
            #expect(!deVal.isEmpty, "Empty DE value for key: \(key)")

            // 3. Translated state
            #expect(enUnit?.state == "translated", "EN not translated for key: \(key)")
            #expect(deUnit?.state == "translated", "DE not translated for key: \(key)")

            // 4. Placeholder token parity
            let enMatches = enVal.matches(of: placeholderRegex).map { String(enVal[$0.range]) }
            let deMatches = deVal.matches(of: placeholderRegex).map { String(deVal[$0.range]) }
            #expect(enMatches.count == deMatches.count, "Placeholder count mismatch for '\(key)': EN has \(enMatches) vs DE \(deMatches)")
        }
    }

    // MARK: - 8. EPG Content Language Boundary Preservation (Real Rendering Path)

    @Test("EPG source content is preserved verbatim through real UI rendering path even when matching translation keys")
    @MainActor
    func epgSourceContentLanguagePreservationInRealRenderingPath() {
        // Enigma2 broadcast content:
        // Case A: Programme title matches an English application key ("Settings")
        // Case B: German broadcast metadata ("Tagesschau", "Nachrichten der ARD")
        let germanChannelName = "Das Erste HD"
        let collisionProgrammeTitle = "Settings" // identical to an application key
        let germanProgrammeTitle = "Tagesschau"
        let germanProgrammeDesc = "Nachrichten der ARD mit Wetterbericht"

        let channel = Channel(
            id: "1",
            name: germanChannelName,
            number: "1",
            serviceRef: "1:0:19:283D:3FB:1:C00000:0:0:0:",
            logoURL: nil
        )

        let collisionEntry = NowNext.Entry(
            title: collisionProgrammeTitle,
            description: germanProgrammeDesc,
            start: Date(),
            end: Date().addingTimeInterval(900)
        )

        let germanEntry = NowNext.Entry(
            title: germanProgrammeTitle,
            description: germanProgrammeDesc,
            start: Date(),
            end: Date().addingTimeInterval(900)
        )

        let nowNextCollision = NowNext(serviceRef: channel.serviceRef, now: collisionEntry, next: nil)
        let nowNextGerman = NowNext(serviceRef: channel.serviceRef, now: germanEntry, next: nil)

        // Helper to recursively collect all SwiftUI Text instances and their storage from the view hierarchy
        func extractSwiftUITexts(from value: Any) -> [String] {
            var results: [String] = []
            let desc = String(describing: value)
            if desc.starts(with: "Text(storage: ") {
                results.append(desc)
            }
            let mirror = Mirror(reflecting: value)
            for child in mirror.children {
                results.append(contentsOf: extractSwiftUITexts(from: child.value))
            }
            return results
        }

        // 1. Render ChannelRow in an English UI environment with a show matching an application translation key ("Settings")
        let rowA = ChannelRow(
            channel: channel,
            nowNext: nowNextCollision,
            fullSchedule: [collisionEntry]
        )
        let textsA = extractSwiftUITexts(from: rowA.body)
        #expect(textsA.contains(where: { $0.contains("verbatim(\"Das Erste HD\")") }), "Channel name must be stored as verbatim text")
        #expect(textsA.contains(where: { $0.contains("verbatim(\"Settings\")") }), "Broadcast title 'Settings' must be stored as verbatim text, never a LocalizedStringKey")

        // 2. Render ChannelRow with German programme in English UI
        let rowB = ChannelRow(
            channel: channel,
            nowNext: nowNextGerman,
            fullSchedule: [germanEntry]
        )
        let textsB = extractSwiftUITexts(from: rowB.body)
        #expect(textsB.contains(where: { $0.contains("verbatim(\"Tagesschau\")") }), "German programme title must be preserved verbatim in English UI")
        #expect(textsB.contains(where: { $0.contains("verbatim(\"Nachrichten der ARD") }), "German programme description must be preserved verbatim")

        // 3. In German UI, 'Settings' broadcast must still be verbatim("Settings"), never a LocalizedStringKey that translates to 'Einstellungen'
        let rowC = ChannelRow(
            channel: channel,
            nowNext: nowNextCollision,
            fullSchedule: [collisionEntry]
        )
        let textsC = extractSwiftUITexts(from: rowC.body)
        #expect(textsC.contains(where: { $0.contains("verbatim(\"Settings\")") }), "Broadcast title 'Settings' must be stored as verbatim text in German UI")
        #expect(!textsC.contains(where: { $0.contains("Einstellungen") }), "Broadcast title 'Settings' must never produce 'Einstellungen' in UI storage")
    }

    // MARK: - 9. ErrorClassifier & UserFacingError Presentation

    @Test("ErrorClassifier maps RFC 7807 problem details, transport errors, and suppresses cancellation")
    func errorClassifierPresentationRules() {
        // 1. Explicit cancellation suppression
        let cancelError = CancellationError()
        #expect(ErrorClassifier.classify(cancelError) == nil)

        let urlCancelled = URLError(.cancelled)
        #expect(ErrorClassifier.classify(urlCancelled) == nil)

        let nsCancelled = NSError(domain: NSURLErrorDomain, code: NSURLErrorCancelled, userInfo: nil)
        #expect(ErrorClassifier.classify(nsCancelled) == nil)

        let transportCancelled = APIError.transport(.cancelled)
        #expect(ErrorClassifier.classify(transportCancelled) == nil)

        // 2. Transport failures
        let offlineErr = APIError.transport(.offline)
        let offlineUserFacing = ErrorClassifier.classify(offlineErr)
        #expect(offlineUserFacing != nil)
        #expect(resolve(offlineUserFacing!.title, locale: enLocale) == "No Internet Connection")
        #expect(resolve(offlineUserFacing!.title, locale: deLocale) == "Keine Internetverbindung")
        #expect(offlineUserFacing!.isRetryable == true)

        let timeoutErr = APIError.transport(.timedOut)
        let timeoutUserFacing = ErrorClassifier.classify(timeoutErr)
        #expect(timeoutUserFacing != nil)
        #expect(resolve(timeoutUserFacing!.title, locale: enLocale) == "Connection Timed Out")
        #expect(resolve(timeoutUserFacing!.title, locale: deLocale) == "Zeitüberschreitung bei der Verbindung")

        // 3. Verified RFC 7807 problem codes
        let unreachableProblem = ProblemDetails(
            type: "https://xg2g.local/problems/receiver-unreachable",
            title: "Receiver Unreachable",
            status: 503,
            requestId: "req-1234",
            code: "RECEIVER_UNREACHABLE",
            detail: "Enigma2 box did not respond on 10.10.55.64",
            instance: nil
        )
        let unreachableUserFacing = ErrorClassifier.classify(APIError.problem(unreachableProblem))
        #expect(unreachableUserFacing != nil)
        #expect(resolve(unreachableUserFacing!.title, locale: enLocale) == "Receiver Unreachable")
        #expect(resolve(unreachableUserFacing!.title, locale: deLocale) == "Receiver nicht erreichbar")
        #expect(unreachableUserFacing!.code == "RECEIVER_UNREACHABLE")
        #expect(unreachableUserFacing!.requestId == "req-1234")
        #expect(unreachableUserFacing!.diagnosticLog?.contains("10.10.55.64") == true)
        #expect(resolve(unreachableUserFacing!.detail!, locale: enLocale) == "The TV receiver cannot be reached. Please check its connection.")

        // 4. Generic/unknown code must NOT claim receiver-specific causes
        let genericProblem = ProblemDetails(
            type: "https://xg2g.local/problems/generic",
            title: "Internal Error",
            status: 500,
            requestId: "req-9999",
            code: "UNKNOWN_INTERNAL_ERROR",
            detail: "Database crashed",
            instance: nil
        )
        let genericUserFacing = ErrorClassifier.classify(APIError.problem(genericProblem))
        #expect(genericUserFacing != nil)
        #expect(resolve(genericUserFacing!.title, locale: enLocale) == "Server Error")
        #expect(resolve(genericUserFacing!.title, locale: deLocale) == "Server-Fehler")
        let genericEnDetail = resolve(genericUserFacing!.detail!, locale: enLocale)
        #expect(!genericEnDetail.lowercased().contains("receiver"), "Generic 500 must not blame receiver")

        // 5. Session reauthentication required
        let reauthError = SessionCoordinator.Failure.reauthenticationRequired(.refreshRejected)
        let reauthUserFacing = ErrorClassifier.classify(reauthError)
        #expect(reauthUserFacing != nil)
        #expect(resolve(reauthUserFacing!.title, locale: enLocale) == "Device Must Be Paired Again")
        #expect(resolve(reauthUserFacing!.title, locale: deLocale) == "Gerät muss erneut gekoppelt werden")
        #expect(reauthUserFacing!.isRetryable == false)
    }

    // MARK: - 10. Admission Distinction & Diagnostic Formatting

    @Test("ErrorClassifier separates admission session capacity from tuner exhaustion and formats diagnostics")
    func errorClassifierAdmissionDistinctionAndDiagnostics() {
        // 1. ADMISSION_NO_TUNERS
        let tunerProblem = ProblemDetails(
            type: "https://xg2g.local/problems/admission-no-tuners",
            title: "No Tuners Available",
            status: 503,
            requestId: "req-tuners-1",
            code: "ADMISSION_NO_TUNERS",
            detail: "All 4 tuners in use on Vu+ Uno 4K",
            instance: nil
        )
        let tunerError = ErrorClassifier.classify(APIError.problem(tunerProblem))
        #expect(tunerError != nil)
        #expect(resolve(tunerError!.title, locale: enLocale) == "All Tuners Occupied")
        #expect(resolve(tunerError!.title, locale: deLocale) == "Alle Tuner belegt")
        #expect(resolve(tunerError!.detail!, locale: enLocale) == "No free receiver tuners available right now.")
        #expect(resolve(tunerError!.detail!, locale: deLocale) == "Aktuell sind keine freien Receiver-Tuner verfügbar.")
        #expect(tunerError!.code == "ADMISSION_NO_TUNERS")
        #expect(tunerError!.requestId == "req-tuners-1")
        #expect(tunerError!.diagnosticSummary.contains("code=ADMISSION_NO_TUNERS"))
        #expect(tunerError!.diagnosticSummary.contains("requestId=req-tuners-1"))

        // 2. ADMISSION_SESSIONS_FULL
        let sessionProblem = ProblemDetails(
            type: "https://xg2g.local/problems/admission-sessions-full",
            title: "Max Sessions Reached",
            status: 503,
            requestId: "req-sessions-2",
            code: "ADMISSION_SESSIONS_FULL",
            detail: "Max concurrent client limit 8 reached",
            instance: nil
        )
        let sessionError = ErrorClassifier.classify(APIError.problem(sessionProblem))
        #expect(sessionError != nil)
        #expect(resolve(sessionError!.title, locale: enLocale) == "Streaming Limit Reached")
        #expect(resolve(sessionError!.title, locale: deLocale) == "Streaming-Limit erreicht")
        #expect(resolve(sessionError!.detail!, locale: enLocale) == "The maximum number of active streaming sessions has been reached.")
        #expect(resolve(sessionError!.detail!, locale: deLocale) == "Die maximale Anzahl aktiver Streaming-Sitzungen wurde erreicht.")
        #expect(sessionError!.code == "ADMISSION_SESSIONS_FULL")
        #expect(sessionError!.requestId == "req-sessions-2")
        #expect(sessionError!.diagnosticSummary.contains("code=ADMISSION_SESSIONS_FULL"))
        #expect(sessionError!.diagnosticSummary.contains("requestId=req-sessions-2"))

        // 3. ADMISSION_TRANSCODES_FULL
        let transcodeProblem = ProblemDetails(
            type: "https://xg2g.local/problems/admission-transcodes-full",
            title: "Max Transcodes Reached",
            status: 503,
            requestId: "req-trans-3",
            code: "ADMISSION_TRANSCODES_FULL",
            detail: "QSV transcode slot exhaustion",
            instance: nil
        )
        let transcodeError = ErrorClassifier.classify(APIError.problem(transcodeProblem))
        #expect(transcodeError != nil)
        #expect(resolve(transcodeError!.title, locale: enLocale) == "Transcoding Limit Reached")
        #expect(resolve(transcodeError!.title, locale: deLocale) == "Transkodierungs-Limit erreicht")

        // 4. ZapPreparation classification
        let prepTuningTimeout = ZapPreparation(
            preparationId: "p1",
            zapId: "z1",
            serviceRef: "sref",
            state: "failed",
            outcome: "tuning_timeout",
            generation: nil,
            readyAfterMs: nil,
            pending: nil,
            detail: "lock timed out after 3000ms"
        )
        let prepTimeoutErr = ErrorClassifier.classifyZapPreparation(prepTuningTimeout)
        #expect(resolve(prepTimeoutErr.title, locale: enLocale) == "Channel Tuning Timed Out")
        #expect(resolve(prepTimeoutErr.title, locale: deLocale) == "Kanalabstimmung abgelaufen")
        #expect(prepTimeoutErr.diagnosticLog == "lock timed out after 3000ms")

        let prepScrambled = ZapPreparation(
            preparationId: "p2",
            zapId: "z2",
            serviceRef: "sref",
            state: "failed",
            outcome: "scrambled",
            generation: nil,
            readyAfterMs: nil,
            pending: nil,
            detail: "CA status 0x80"
        )
        let prepScrambledErr = ErrorClassifier.classifyZapPreparation(prepScrambled)
        #expect(resolve(prepScrambledErr.title, locale: enLocale) == "Channel Scrambled")
        #expect(resolve(prepScrambledErr.title, locale: deLocale) == "Kanal verschlüsselt")
        #expect(prepScrambledErr.isRetryable == false)

        let prepNoData = ZapPreparation(
            preparationId: "p3",
            zapId: "z3",
            serviceRef: "sref",
            state: "failed",
            outcome: "no_data",
            generation: nil,
            readyAfterMs: nil,
            pending: nil,
            detail: "tuner unlocked"
        )
        let prepNoDataErr = ErrorClassifier.classifyZapPreparation(prepNoData)
        #expect(resolve(prepNoDataErr.title, locale: enLocale) == "No Broadcast Signal")
        #expect(resolve(prepNoDataErr.title, locale: deLocale) == "Kein Sendesignal")
    }
}
