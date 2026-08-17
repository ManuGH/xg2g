// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import UIKit

/// High-performance centralized Haptic Feedback engine.
///
/// Pre-warms UIKit feedback generators and re-arms them after impact,
/// eliminating the 10-30 ms allocation & spin-up lag on touch interactions.
@MainActor
public final class Haptics {

    public static let shared = Haptics()

    private let lightImpact = UIImpactFeedbackGenerator(style: .light)
    private let mediumImpact = UIImpactFeedbackGenerator(style: .medium)
    private let heavyImpact = UIImpactFeedbackGenerator(style: .heavy)
    private let selectionFeedback = UISelectionFeedbackGenerator()
    private let notificationFeedback = UINotificationFeedbackGenerator()

    private init() {
        prepareAll()
    }

    public func prepareAll() {
        lightImpact.prepare()
        mediumImpact.prepare()
        selectionFeedback.prepare()
    }

    /// Triggers an impact feedback and re-arms the generator for immediate follow-up touches.
    public func impact(_ style: UIImpactFeedbackGenerator.FeedbackStyle = .light) {
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
        selectionFeedback.selectionChanged()
        selectionFeedback.prepare()
    }

    /// Triggers a notification feedback (e.g. .success for timer programmed, .error on failure).
    public func notification(_ type: UINotificationFeedbackGenerator.FeedbackType) {
        notificationFeedback.notificationOccurred(type)
        notificationFeedback.prepare()
    }
}
