// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

struct PlatformCapabilitiesTests {

    @Test("InteractionModel defines all four canonical interaction paradigms")
    func testInteractionModelCases() {
        let cases = InteractionModel.allCases
        #expect(cases.count == 4)
        #expect(cases.contains(.touchCompact))
        #expect(cases.contains(.touchRegular))
        #expect(cases.contains(.focusRemote))
        #expect(cases.contains(.pointerKeyboard))
    }

    @Test("PlatformCapabilities resolves current runtime environment deterministically")
    @MainActor
    func testPlatformCapabilitiesCurrent() {
        let current = PlatformCapabilities.current
        #expect([.touchCompact, .touchRegular, .focusRemote, .pointerKeyboard].contains(current.interactionModel))

        #if os(tvOS)
        #expect(current.interactionModel == .focusRemote)
        #expect(current.supportsHaptics == false)
        #expect(current.supportsHover == false)
        #endif

        #if os(macOS) || targetEnvironment(macCatalyst)
        #expect(current.interactionModel == .pointerKeyboard)
        #expect(current.supportsHover == true)
        #expect(current.supportsHardwareKeyboard == true)
        #expect(current.supportsHaptics == false)
        #endif
    }

    @Test("Explicit PlatformCapabilities instantiation maintains contract integrity")
    func testExplicitPlatformCapabilities() {
        let caps = PlatformCapabilities(
            interactionModel: .touchCompact,
            supportsPictureInPicture: true,
            supportsHover: false,
            supportsHardwareKeyboard: false,
            supportsHaptics: true
        )

        #expect(caps.interactionModel == .touchCompact)
        #expect(caps.supportsPictureInPicture == true)
        #expect(caps.supportsHover == false)
        #expect(caps.supportsHardwareKeyboard == false)
        #expect(caps.supportsHaptics == true)
    }

    @Test("InteractionModel touchRegular and pointerKeyboard route to iPad and Mac capabilities")
    func testIPadAndMacCapabilitiesRouting() {
        let padCaps = PlatformCapabilities(
            interactionModel: .touchRegular,
            supportsPictureInPicture: true,
            supportsHover: true,
            supportsHardwareKeyboard: true,
            supportsHaptics: false
        )
        #expect(padCaps.interactionModel == .touchRegular)
        #expect(padCaps.supportsHover == true)
        #expect(padCaps.supportsHardwareKeyboard == true)
        #expect(padCaps.supportsHaptics == false)

        let macCaps = PlatformCapabilities(
            interactionModel: .pointerKeyboard,
            supportsPictureInPicture: true,
            supportsHover: true,
            supportsHardwareKeyboard: true,
            supportsHaptics: false
        )
        #expect(macCaps.interactionModel == .pointerKeyboard)
        #expect(macCaps.supportsHover == true)
        #expect(macCaps.supportsHardwareKeyboard == true)
    }

    @Test("DeviceCapabilities.classifyPlatform maps machine identifiers to contract platforms deterministically")
    func testClassifyPlatformMappings() {
        #expect(DeviceCapabilities.classifyPlatform(machineIdentifier: "iPhone17,1", isiOSAppOnMac: false) == .ios)
        #expect(DeviceCapabilities.classifyPlatform(machineIdentifier: "iPad16,6", isiOSAppOnMac: false) == .ipados)
        #expect(DeviceCapabilities.classifyPlatform(machineIdentifier: "AppleTV14,1", isiOSAppOnMac: false) == .tvos)
        #expect(DeviceCapabilities.classifyPlatform(machineIdentifier: "iPad16,6", isiOSAppOnMac: true) == .macos)
        #expect(DeviceCapabilities.classifyPlatform(machineIdentifier: "arm64", isiOSAppOnMac: false) == .ios)
    }

    @Test("DeviceCapabilities.deviceContext osName matches clientPlatform rawValue")
    func testDeviceContextOsNameMatchesClientPlatform() {
        #expect(DeviceCapabilities.deviceContext.osName == DeviceCapabilities.clientPlatform.rawValue)
    }
}
