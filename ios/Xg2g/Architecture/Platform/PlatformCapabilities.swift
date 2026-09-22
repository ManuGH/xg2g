// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
#if canImport(UIKit)
import UIKit
#endif
#if canImport(AVKit)
import AVKit
#endif

/// Comprehensive platform and hardware capabilities for the host device.
struct PlatformCapabilities: Sendable, Equatable {
    let interactionModel: InteractionModel
    let supportsPictureInPicture: Bool
    let supportsHover: Bool
    let supportsHardwareKeyboard: Bool
    let supportsHaptics: Bool

    init(
        interactionModel: InteractionModel,
        supportsPictureInPicture: Bool,
        supportsHover: Bool,
        supportsHardwareKeyboard: Bool,
        supportsHaptics: Bool
    ) {
        self.interactionModel = interactionModel
        self.supportsPictureInPicture = supportsPictureInPicture
        self.supportsHover = supportsHover
        self.supportsHardwareKeyboard = supportsHardwareKeyboard
        self.supportsHaptics = supportsHaptics
    }

    /// Resolves the capabilities of the current runtime environment.
    @MainActor
    static var current: PlatformCapabilities {
        #if os(tvOS)
        return PlatformCapabilities(
            interactionModel: .focusRemote,
            supportsPictureInPicture: false,
            supportsHover: false,
            supportsHardwareKeyboard: false,
            supportsHaptics: false
        )
        #elseif os(macOS) || targetEnvironment(macCatalyst)
        return PlatformCapabilities(
            interactionModel: .pointerKeyboard,
            supportsPictureInPicture: AVPictureInPictureController.isPictureInPictureSupported(),
            supportsHover: true,
            supportsHardwareKeyboard: true,
            supportsHaptics: false
        )
        #else
        let isMac = ProcessInfo.processInfo.isiOSAppOnMac
        let isPad = UIDevice.current.userInterfaceIdiom == .pad

        if isMac {
            return PlatformCapabilities(
                interactionModel: .pointerKeyboard,
                supportsPictureInPicture: AVPictureInPictureController.isPictureInPictureSupported(),
                supportsHover: true,
                supportsHardwareKeyboard: true,
                supportsHaptics: false
            )
        } else if isPad {
            return PlatformCapabilities(
                interactionModel: .touchRegular,
                supportsPictureInPicture: AVPictureInPictureController.isPictureInPictureSupported(),
                supportsHover: true,
                supportsHardwareKeyboard: true,
                supportsHaptics: false
            )
        } else {
            return PlatformCapabilities(
                interactionModel: .touchCompact,
                supportsPictureInPicture: AVPictureInPictureController.isPictureInPictureSupported(),
                supportsHover: false,
                supportsHardwareKeyboard: false,
                supportsHaptics: true
            )
        }
        #endif
    }
}
