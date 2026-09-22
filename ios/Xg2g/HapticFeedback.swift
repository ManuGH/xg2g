// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

#if os(tvOS) || targetEnvironment(macCatalyst)
import Foundation

/// High-performance centralized Haptic Feedback engine (tvOS & Mac Catalyst stub).
@MainActor
public final class Haptics {
    public static let shared = Haptics()

    public enum FeedbackStyle: Sendable {
        case light, medium, heavy, soft, rigid
    }
    public enum FeedbackType: Sendable {
        case success, warning, error
    }

    private init() {}
    public func prepareAll() {}
    public func impact(_ style: FeedbackStyle = .light) {}
    public func selection() {}
    public func notification(_ type: FeedbackType) {}
}
#else
import UIKit

/// High-performance centralized Haptic Feedback engine.
///
/// Pre-warms UIKit feedback generators and re-arms them after impact,
/// eliminating the 10-30 ms allocation & spin-up lag on touch interactions.
@MainActor
public final class Haptics {

    public static let shared = Haptics()

    public typealias FeedbackStyle = UIImpactFeedbackGenerator.FeedbackStyle
    public typealias FeedbackType = UINotificationFeedbackGenerator.FeedbackType

    private let lightImpact = UIImpactFeedbackGenerator(style: .light)
    private let mediumImpact = UIImpactFeedbackGenerator(style: .medium)
    private let heavyImpact = UIImpactFeedbackGenerator(style: .heavy)
    private let selectionFeedback = UISelectionFeedbackGenerator()
    private let notificationFeedback = UINotificationFeedbackGenerator()

    private init() {
        prepareAll()
    }

    public func prepareAll() {
        guard !ProcessInfo.processInfo.isiOSAppOnMac else { return }
        lightImpact.prepare()
        mediumImpact.prepare()
        selectionFeedback.prepare()
    }

    /// Triggers an impact feedback and re-arms the generator for immediate follow-up touches.
    public func impact(_ style: FeedbackStyle = .light) {
        guard !ProcessInfo.processInfo.isiOSAppOnMac else { return }
        switch style {
        case .light:
            lightImpact.impactOccurred()
            lightImpact.prepare()
        case .medium:
            mediumImpact.impactOccurred()
            mediumImpact.prepare()
        case .heavy:
            heavyImpact.impactOccurred()
            heavyImpact.prepare()
        case .soft, .rigid:
            let gen = UIImpactFeedbackGenerator(style: style)
            gen.impactOccurred()
        @unknown default:
            break
        }
    }

    /// Triggers a selection click (e.g. for carousels, scrubbers, or wheel pickers).
    public func selection() {
        guard !ProcessInfo.processInfo.isiOSAppOnMac else { return }
        selectionFeedback.selectionChanged()
        selectionFeedback.prepare()
    }

    /// Triggers a notification feedback (e.g. .success for timer programmed, .error on failure).
    public func notification(_ type: FeedbackType) {
        guard !ProcessInfo.processInfo.isiOSAppOnMac else { return }
        notificationFeedback.notificationOccurred(type)
        notificationFeedback.prepare()
    }
}
#endif
