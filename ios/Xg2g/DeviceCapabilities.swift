// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import Foundation
import UIKit

/// Runtime hardware capability inspector for Apple silicon and iOS devices.
///
/// Ensures the client strictly negotiates codecs supported by its local hardware decoder
/// (e.g. AV1 on A17 Pro / M3 / M4, HEVC on A10+), preventing unsupported codec crashes.
enum DeviceCapabilities {

    struct VideoCodecSignal: Encodable, Sendable {
        let codec: String
        let supported: Bool
        let smooth: Bool
        let powerEfficient: Bool
    }

    struct DeviceContext: Encodable, Sendable {
        let brand: String = "Apple"
        let manufacturer: String = "Apple"
        let platform: String = "darwin"
        let model: String
        let osName: String = "ios"
        let osVersion: String
    }

    /// Detects whether the current device hardware supports native AV1 video decoding.
    ///
    /// Supported natively by Apple on:
    /// - iPhone 15 Pro / Pro Max (A17 Pro)
    /// - iPhone 16 / 16 Plus / 16 Pro / 16 Pro Max (A18 / A18 Pro)
    /// - iPhone 17 series
    /// - Apple Silicon M3, M4, and newer (Macs & iPad Pro/Air)
    static var supportsAV1: Bool {
        if #available(iOS 17.0, *) {
            return AVURLAsset.isPlayableExtendedMIMEType("video/mp4; codecs=\"av01.0.08M.10\"")
        }
        return false
    }

    /// Detects whether the device hardware supports native HEVC (H.265) video decoding.
    static var supportsHEVC: Bool {
        if #available(iOS 11.0, *) {
            return AVURLAsset.isPlayableExtendedMIMEType("video/mp4; codecs=\"hvc1.1.6.L93.B0\"")
        }
        return true
    }

    /// Returns the exact list of hardware-accelerated codecs supported on this device.
    static var supportedCodecsList: [String] {
        var codecs: [String] = []
        if supportsAV1 {
            codecs.append("av1")
        }
        if supportsHEVC {
            codecs.append("hevc")
        }
        codecs.append("h264")
        return codecs
    }

    /// Returns structured codec capability signals for backend planner runtime validation.
    static var codecSignals: [VideoCodecSignal] {
        var signals: [VideoCodecSignal] = []
        if supportsAV1 {
            signals.append(VideoCodecSignal(codec: "av1", supported: true, smooth: true, powerEfficient: true))
        }
        if supportsHEVC {
            signals.append(VideoCodecSignal(codec: "hevc", supported: true, smooth: true, powerEfficient: true))
        }
        signals.append(VideoCodecSignal(codec: "h264", supported: true, smooth: true, powerEfficient: true))
        return signals
    }

    /// Hardware context identifier payload (e.g. model "iPhone17,1", OS "18.0").
    /// This build's version, as the client identity reports it.
    ///
    /// Diagnostic only: nothing the server decides is keyed on it, because a
    /// policy that were would make every app release a server change.
    static var appVersion: String {
        Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "unknown"
    }

    static var deviceContext: DeviceContext {
        let version = ProcessInfo.processInfo.operatingSystemVersion
        let osVersion = version.patchVersion > 0
            ? "\(version.majorVersion).\(version.minorVersion).\(version.patchVersion)"
            : "\(version.majorVersion).\(version.minorVersion)"
        return DeviceContext(
            model: machineIdentifier,
            osVersion: osVersion
        )
    }

    /// Native sysctl hw.machine string.
    static var machineIdentifier: String {
        var systemInfo = utsname()
        uname(&systemInfo)
        let machineMirror = Mirror(reflecting: systemInfo.machine)
        let identifier = machineMirror.children.reduce("") { identifier, element in
            guard let value = element.value as? Int8, value != 0 else { return identifier }
            return identifier + String(UnicodeScalar(UInt8(value)))
        }
        return identifier.isEmpty ? "iPhone" : identifier
    }

    /// Returns a comma-separated list of codecs for API negotiation (e.g. "av1,hevc,h264" or "hevc,h264").
    static var supportedCodecsHeader: String {
        supportedCodecsList.joined(separator: ",")
    }

    /// Human-readable description of current hardware decoding capabilities.
    static var hardwareSummary: String {
        let codecs = supportedCodecsList.map { $0.uppercased() }.joined(separator: ", ")
        return "HW-Decoder: \(codecs)"
    }
}
