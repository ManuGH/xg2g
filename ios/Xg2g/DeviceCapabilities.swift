// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import Foundation

/// Runtime hardware capability inspector for Apple silicon and iOS devices.
///
/// Ensures the client strictly negotiates codecs supported by its local hardware decoder
/// (e.g. AV1 on A17 Pro / M3 / M4, HEVC on A10+), preventing unsupported codec crashes.
enum DeviceCapabilities {

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
