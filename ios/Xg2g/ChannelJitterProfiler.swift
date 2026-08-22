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
/// - Remembers recent worst socket stall intervals per channel across app sessions via UserDefaults.
/// - Uncached/unknown channels start with a safe conservative cushion (~850ms).
/// - Channels with verified low jitter (e.g. Sky/PULS) get fast-tracked with low initial pre-roll (~350ms).
/// - Channels with recurring stall bursts (e.g. ORF 1 HD with ~800ms gaps) are automatically allocated a robust pre-roll (~900ms).
/// - Profile values decay very slowly (half-life over minutes/hours) so problematic feeds do not prematurely regress.
public final class ChannelJitterProfiler: @unchecked Sendable {
    public static let shared = ChannelJitterProfiler()

    private let lock = NSLock()

    public struct ProfileEntry: Codable, Sendable {
        public var recentWorstStallMs: Double
        public var lastObservedAtEpoch: Double
        public var zapCount: Int
        /// Zaps to this channel since the last observed stall.
        ///
        /// A channel that has been watched repeatedly without stalling has been
        /// measured, not left unmeasured, and the two used to be indistinguishable:
        /// the recommendation keyed on `recentWorstStallMs > 0`, so a channel that
        /// behaved perfectly never left the conservative default.
        public var stallFreeZaps: Int

        public init(recentWorstStallMs: Double, lastObservedAtEpoch: Double, zapCount: Int, stallFreeZaps: Int = 0) {
            self.recentWorstStallMs = recentWorstStallMs
            self.lastObservedAtEpoch = lastObservedAtEpoch
            self.zapCount = zapCount
            self.stallFreeZaps = stallFreeZaps
        }

        /// Decoded leniently so profiles persisted before this field existed keep
        /// their stall history instead of being discarded wholesale.
        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            recentWorstStallMs = try c.decode(Double.self, forKey: .recentWorstStallMs)
            lastObservedAtEpoch = try c.decode(Double.self, forKey: .lastObservedAtEpoch)
            zapCount = try c.decode(Int.self, forKey: .zapCount)
            stallFreeZaps = try c.decodeIfPresent(Int.self, forKey: .stallFreeZaps) ?? 0
        }
    }

    private var profiles: [String: ProfileEntry] = [:]
    private static let userDefaultsKey = "io.github.manugh.xg2g.channelJitterProfiles"

    public static let defaultPreRollSeconds: Double = 0.85
    public static let minPreRollSeconds: Double = 0.35
    public static let maxPreRollSeconds: Double = 0.90
    public static let safetyMarginMs: Double = 150.0
    /// The rate at which stall history loses weight. Used both for decaying a stale
    /// worst-stall observation and for walking the cushion down over stall-free zaps,
    /// so the two directions move at the same pace.
    public static let stallDecayFactor: Double = 0.90

    private init() {
        loadFromPersistence()
    }

    private func loadFromPersistence() {
        guard let data = UserDefaults.standard.data(forKey: Self.userDefaultsKey) else { return }
        do {
            let decoded = try JSONDecoder().decode([String: ProfileEntry].self, from: data)
            profiles = decoded
            let msg = "[JitterProfiler] 💾 Loaded \(decoded.count) channel profile(s) from persistent storage."
            logger.notice("\(msg, privacy: .public)")
        } catch {
            logger.error("[JitterProfiler] ⚠️ Failed to decode persisted profiles: \(error.localizedDescription, privacy: .public)")
        }
    }

    private func saveToPersistenceLocked() {
        do {
            let data = try JSONEncoder().encode(profiles)
            UserDefaults.standard.set(data, forKey: Self.userDefaultsKey)
        } catch {
            logger.error("[JitterProfiler] ⚠️ Failed to persist profiles: \(error.localizedDescription, privacy: .public)")
        }
    }

    /// Records an observed socket stall for the given channel key.
    public func recordStall(for channelKey: String, stallMs: Double) {
        lock.lock()
        defer { lock.unlock() }

        let nowEpoch = Date().timeIntervalSince1970
        var entry = profiles[channelKey] ?? ProfileEntry(recentWorstStallMs: 0, lastObservedAtEpoch: nowEpoch, zapCount: 0)

        // Slow decay: if last observation is older than 10 minutes, decay by 10%
        let elapsed = nowEpoch - entry.lastObservedAtEpoch
        if elapsed > 600.0 {
            entry.recentWorstStallMs *= Self.stallDecayFactor
        }

        entry.recentWorstStallMs = max(entry.recentWorstStallMs, stallMs)
        entry.lastObservedAtEpoch = nowEpoch
        entry.stallFreeZaps = 0
        profiles[channelKey] = entry
        saveToPersistenceLocked()

        let msg = "[JitterProfiler] 📝 Profile updated for \(channelKey): worst stall = \(String(format: "%.0f", entry.recentWorstStallMs))ms (zap count: \(entry.zapCount))"
        logger.notice("\(msg, privacy: .public)")
    }

    /// Notes a zap to this channel to track experience count.
    public func noteZap(for channelKey: String) {
        lock.lock()
        defer { lock.unlock() }

        let nowEpoch = Date().timeIntervalSince1970
        var entry = profiles[channelKey] ?? ProfileEntry(recentWorstStallMs: 0, lastObservedAtEpoch: nowEpoch, zapCount: 0)
        entry.zapCount += 1
        entry.stallFreeZaps += 1
        profiles[channelKey] = entry
        saveToPersistenceLocked()
    }

    /// Recommends the optimal initial audio pre-roll for this channel.
    public func recommendedAudioPreRoll(for channelKey: String) -> (preRollSeconds: Double, reason: String) {
        if channelKey.contains("/stream/smooth") {
            return (Self.minPreRollSeconds, "smoothed backend stream (350ms quick-zap cushion)")
        }

        lock.lock()
        defer { lock.unlock() }

        guard let entry = profiles[channelKey], entry.zapCount > 0 else {
            return (Self.defaultPreRollSeconds, "unknown channel / no history (conservative default)")
        }

        // What the observed stalls demand. With no stall on record this is the
        // safety margin alone, which the floor then raises to the minimum cushion.
        let requiredMs = entry.recentWorstStallMs + Self.safetyMarginMs
        let demanded = min(Self.maxPreRollSeconds, max(Self.minPreRollSeconds, requiredMs / 1000.0))

        // Stall-free zaps are evidence, and evidence accumulates rather than
        // arriving all at once: a single clean zap does not prove a channel never
        // stalls. The cushion is walked down from the conservative default by the
        // same 0.90 factor the stall history already decays with, so no new
        // tuning constant is introduced, and it can never fall below what the
        // observed stalls demand.
        let earned = Self.defaultPreRollSeconds * pow(Self.stallDecayFactor, Double(entry.stallFreeZaps))
        let preRoll = min(Self.maxPreRollSeconds, max(demanded, earned))

        let reason: String
        if entry.recentWorstStallMs > 0 {
            reason = "learned persistent profile (worst stall: \(String(format: "%.0f", entry.recentWorstStallMs))ms + \(String(format: "%.0f", Self.safetyMarginMs))ms margin)"
        } else {
            reason = "\(entry.stallFreeZaps) stall-free zap(s) on record, no stall ever observed"
        }
        return (preRoll, reason)
    }
}
