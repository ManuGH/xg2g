// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation
import OSLog
import QuartzCore

private let logger = Logger(subsystem: "io.github.manugh.xg2g.ios", category: "jitter-profiler")

/// Profiles network jitter and Enigma2 socket stall history per channel/service reference.
///
/// Principles:
/// - Remembers recent worst socket stall intervals per channel.
/// - Uncached/unknown channels start with a safe conservative cushion.
/// - Channels with verified low jitter (e.g. Sky/PULS) get fast-tracked with low initial pre-roll (~350ms).
/// - Channels with recurring stall bursts (e.g. ORF 1 HD with 800ms gaps) are automatically allocated a robust pre-roll (~900ms).
/// - Profile values decay very slowly (half-life over minutes/hours) so problematic feeds do not prematurely regress.
public final class ChannelJitterProfiler: @unchecked Sendable {
    public static let shared = ChannelJitterProfiler()

    private let lock = NSLock()

    public struct ProfileEntry: Sendable {
        public var recentWorstStallMs: Double
        public var lastObservedAt: CFTimeInterval
        public var zapCount: Int
    }

    private var profiles: [String: ProfileEntry] = [:]

    public static let defaultPreRollSeconds: Double = 0.85
    public static let minPreRollSeconds: Double = 0.35
    public static let maxPreRollSeconds: Double = 0.90
    public static let safetyMarginMs: Double = 150.0

    private init() {}

    /// Records an observed socket stall for the given channel key.
    public func recordStall(for channelKey: String, stallMs: Double) {
        lock.lock()
        defer { lock.unlock() }

        let now = CACurrentMediaTime()
        var entry = profiles[channelKey] ?? ProfileEntry(recentWorstStallMs: 0, lastObservedAt: now, zapCount: 0)

        // Slow decay: if last observation is older than 5 minutes, decay by 10%
        let elapsed = now - entry.lastObservedAt
        if elapsed > 300.0 {
            entry.recentWorstStallMs *= 0.90
        }

        entry.recentWorstStallMs = max(entry.recentWorstStallMs, stallMs)
        entry.lastObservedAt = now
        profiles[channelKey] = entry

        let msg = "[JitterProfiler] 📝 Profile updated for \(channelKey): worst stall = \(String(format: "%.0f", entry.recentWorstStallMs))ms (zap count: \(entry.zapCount))"
        logger.notice("\(msg, privacy: .public)")
    }

    /// Notes a zap to this channel to track experience count.
    public func noteZap(for channelKey: String) {
        lock.lock()
        defer { lock.unlock() }

        let now = CACurrentMediaTime()
        var entry = profiles[channelKey] ?? ProfileEntry(recentWorstStallMs: 0, lastObservedAt: now, zapCount: 0)
        entry.zapCount += 1
        profiles[channelKey] = entry
    }

    /// Recommends the optimal initial audio pre-roll for this channel.
    public func recommendedAudioPreRoll(for channelKey: String) -> (preRollSeconds: Double, reason: String) {
        lock.lock()
        defer { lock.unlock() }

        guard let entry = profiles[channelKey], entry.zapCount > 0, entry.recentWorstStallMs > 0 else {
            return (Self.defaultPreRollSeconds, "unknown channel / no history (conservative default)")
        }

        let requiredMs = entry.recentWorstStallMs + Self.safetyMarginMs
        let preRoll = min(Self.maxPreRollSeconds, max(Self.minPreRollSeconds, requiredMs / 1000.0))
        let reason = "learned profile (worst stall: \(String(format: "%.0f", entry.recentWorstStallMs))ms + \(String(format: "%.0f", Self.safetyMarginMs))ms margin)"
        return (preRoll, reason)
    }
}
