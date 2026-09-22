// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI
import Observation

/// Composition root and dependency container for Greenfield Apple architecture.
///
/// Injects domain-specific stores and platform services into the SwiftUI environment,
/// replacing monolithic dependency on `AppModel`.
@MainActor
@Observable
final class AppComposition {
    let capabilities: PlatformCapabilities
    let feedback: any FeedbackServicing
    let presentation: any PlaybackPresentationServicing
    let guideStore: GuideStore
    let dvrStore: DVRStore
    let playbackStore: PlaybackStore
    let deviceStore: DeviceStore

    init(
        capabilities: PlatformCapabilities = .current,
        feedback: any FeedbackServicing = DefaultFeedbackService(),
        presentation: any PlaybackPresentationServicing = DefaultPlaybackPresentationService(),
        guideStore: GuideStore,
        dvrStore: DVRStore,
        playbackStore: PlaybackStore,
        deviceStore: DeviceStore
    ) {
        self.capabilities = capabilities
        self.feedback = feedback
        self.presentation = presentation
        self.guideStore = guideStore
        self.dvrStore = dvrStore
        self.playbackStore = playbackStore
        self.deviceStore = deviceStore
    }

    /// Factory method bridging Greenfield architecture to the existing AppModel runtime.
    static func makeBridged(appModel: AppModel) -> AppComposition {
        let guideRepo = LegacyBridgeGuideRepository(appModel: appModel)
        let dvrRepo = LegacyBridgeDVRRepository(appModel: appModel)
        let playbackController = LegacyBridgePlaybackController(appModel: appModel)
        let deviceSession = LegacyBridgeDeviceSession(appModel: appModel)

        let guideStore = GuideStore(repository: guideRepo)
        let dvrStore = DVRStore(repository: dvrRepo)
        let playbackStore = PlaybackStore(controller: playbackController)
        let deviceStore = DeviceStore(session: deviceSession)

        return AppComposition(
            capabilities: .current,
            feedback: DefaultFeedbackService(),
            presentation: DefaultPlaybackPresentationService(),
            guideStore: guideStore,
            dvrStore: dvrStore,
            playbackStore: playbackStore,
            deviceStore: deviceStore
        )
    }
}

// MARK: - SwiftUI Environment Keys

private struct PlatformCapabilitiesKey: EnvironmentKey {
    static let defaultValue: PlatformCapabilities = PlatformCapabilities(
        interactionModel: .touchCompact,
        supportsPictureInPicture: true,
        supportsHover: false,
        supportsHardwareKeyboard: false,
        supportsHaptics: true
    )
}

extension EnvironmentValues {
    var platformCapabilities: PlatformCapabilities {
        get { self[PlatformCapabilitiesKey.self] }
        set { self[PlatformCapabilitiesKey.self] = newValue }
    }
}

// MARK: - View Extension

extension View {
    /// Injects all domain stores and platform services from the composition root.
    @MainActor
    func withAppComposition(_ composition: AppComposition) -> some View {
        self
            .environment(composition)
            .environment(composition.guideStore)
            .environment(composition.dvrStore)
            .environment(composition.playbackStore)
            .environment(composition.deviceStore)
            .environment(\.platformCapabilities, composition.capabilities)
    }
}
