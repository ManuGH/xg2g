// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Intensity style for impact haptic / auditory feedback.
enum FeedbackStyle: Sendable, Equatable {
    case light
    case medium
    case heavy
}

/// Notification type for operation feedback.
enum FeedbackNotificationType: Sendable, Equatable {
    case success
    case warning
    case error
}

/// Platform-agnostic feedback service for haptics and interaction cues.
@MainActor
protocol FeedbackServicing: AnyObject {
    func triggerSelection()
    func triggerImpact(_ style: FeedbackStyle)
    func triggerNotification(_ type: FeedbackNotificationType)
}

/// Default production feedback service utilizing Haptics on iOS and no-op on tvOS / Mac.
@MainActor
final class DefaultFeedbackService: FeedbackServicing {
    init() {}

    func triggerSelection() {
        #if !os(tvOS) && !targetEnvironment(macCatalyst)
        guard !ProcessInfo.processInfo.isiOSAppOnMac else { return }
        Haptics.shared.selection()
        #endif
    }

    func triggerImpact(_ style: FeedbackStyle) {
        #if !os(tvOS) && !targetEnvironment(macCatalyst)
        guard !ProcessInfo.processInfo.isiOSAppOnMac else { return }
        switch style {
        case .light:
            Haptics.shared.impact(.light)
        case .medium:
            Haptics.shared.impact(.medium)
        case .heavy:
            Haptics.shared.impact(.heavy)
        }
        #endif
    }

    func triggerNotification(_ type: FeedbackNotificationType) {
        #if !os(tvOS) && !targetEnvironment(macCatalyst)
        guard !ProcessInfo.processInfo.isiOSAppOnMac else { return }
        switch type {
        case .success:
            Haptics.shared.notification(.success)
        case .warning:
            Haptics.shared.notification(.warning)
        case .error:
            Haptics.shared.notification(.error)
        }
        #endif
    }
}
