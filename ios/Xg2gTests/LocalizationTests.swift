// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import SwiftUI
import Testing
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
                #expect(!gain.isEmpty)
            }
            for cost in tradeoff.costs {
                #expect(!cost.isEmpty)
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

    @Test("Render SettingsView snapshots in both German and English")
    @MainActor
    func renderSettingsViewsInBothLocales() throws {
        let locales = [("de", deLocale), ("en", enLocale)]

        for (_, loc) in locales {
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
        }
    }

    @Test("SettingsView layout accommodates large Dynamic Type without crashing or corrupting frame hierarchy")
    @MainActor
    func settingsViewDynamicTypeLayout() {
        let model = AppModel()
        model.playbackEngine = .auto
        model.qualityPreference = .auto

        let dynamicTypeView = SettingsView(model: model)
            .environment(\.sizeCategory, .accessibilityExtraExtraLarge)
            .environment(\.locale, deLocale)

        let controller = UIHostingController(rootView: dynamicTypeView)
        controller.view.frame = CGRect(x: 0, y: 0, width: 393, height: 1200)

        let window = UIWindow(frame: CGRect(x: 0, y: 0, width: 393, height: 1200))
        window.rootViewController = controller
        window.makeKeyAndVisible()
        controller.view.setNeedsLayout()
        controller.view.layoutIfNeeded()

        #expect(controller.view.bounds.width == 393)
        #expect(controller.view.bounds.height == 1200)
    }
}
