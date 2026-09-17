// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

extension AppModel {

    // MARK: - EPG Targets

    static func primeTimeTarget(for date: Date = .now) -> Date {
        let calendar = Calendar.current
        var components = calendar.dateComponents([.year, .month, .day], from: date)
        components.hour = 20
        components.minute = 15
        components.second = 0
        return calendar.date(from: components) ?? date
    }

    static func lateNightTarget(for date: Date = .now) -> Date {
        let calendar = Calendar.current
        var components = calendar.dateComponents([.year, .month, .day], from: date)
        components.hour = 22
        components.minute = 0
        components.second = 0
        return calendar.date(from: components) ?? date
    }

    // MARK: - Show Resolution

    /// Resolves the programme running on a channel for the active time filter
    func show(for channel: Channel, at filter: TimeFilter) -> NowNext.Entry? {
        let scheduleItem = schedule[channel.serviceRef]
        let allShows = fullEpg[channel.serviceRef] ?? []

        switch filter {
        case .now:
            let currentTime = Date.now
            if let now = scheduleItem?.now, now.start <= currentTime && now.end > currentTime {
                return now
            }
            if let current = allShows.first(where: { $0.start <= currentTime && $0.end > currentTime }) {
                return current
            }
            return scheduleItem?.now
        case .next:
            let currentTime = Date.now
            if let next = scheduleItem?.next, next.start >= currentTime {
                return next
            }
            if let current = show(for: channel, at: .now),
               let upcoming = allShows.first(where: { $0.start >= current.end }) {
                return upcoming
            }
            return scheduleItem?.next
        case .primeTimeTonight:
            let target = Self.primeTimeTarget()
            return allShows.first { $0.start <= target && $0.end > target }
                ?? allShows.first { $0.start >= target }
                ?? (allShows.isEmpty ? scheduleItem?.now : nil)
        case .lateNightTonight:
            let target = Self.lateNightTarget()
            return allShows.first { $0.start <= target && $0.end > target }
                ?? allShows.first { $0.start >= target }
                ?? (allShows.isEmpty ? scheduleItem?.now : nil)
        case .day(let dayDate):
            let target = Self.primeTimeTarget(for: dayDate)
            let calendar = Calendar.current
            return allShows.first { $0.start <= target && $0.end > target }
                ?? allShows.first { calendar.isDate($0.start, inSameDayAs: dayDate) }
                ?? allShows.first { $0.start >= target }
                ?? (allShows.isEmpty ? scheduleItem?.now : nil)
        }
    }

    // MARK: - Schedule & Rerun Management

    /// Refresh the live Now/Next EPG schedule for specific channels or all channels.
    func refreshSchedule(for serviceRefs: [String] = []) async {
        let targets = serviceRefs.isEmpty ? channels.map(\.serviceRef) : serviceRefs
        guard !targets.isEmpty else { return }
        if let updated = try? await channelRepository?.nowNext(for: targets) {
            // One merge rather than a per-key loop: each individual assignment
            // would be its own observation notification.
            schedule.merge(updated) { _, new in new }
            lastDataRefreshTime = Date()
        }
    }

    /// Returns the full chronological schedule for a given channel.
    func channelSchedule(for channel: Channel) -> [NowNext.Entry] {
        if let list = fullEpg[channel.serviceRef], !list.isEmpty {
            return list.sorted { $0.start < $1.start }
        }
        var list: [NowNext.Entry] = []
        if let nn = schedule[channel.serviceRef] {
            if let now = nn.now { list.append(now) }
            if let next = nn.next { list.append(next) }
        }
        return list
    }

    /// Finds all other airings / reruns of a given show across all channels in the full EPG buffer.
    func findReruns(for entry: NowNext.Entry, excludingChannelID: String? = nil) -> [RerunItem] {
        let targetNorm = normalizeShowTitle(entry.title)
        guard targetNorm.count >= 3 else { return [] }

        var results: [RerunItem] = []

        for channel in channels {
            let shows = fullEpg[channel.serviceRef] ?? []
            for show in shows {
                // Skip the exact same event instance if on the same channel
                if show.id == entry.id && channel.id == excludingChannelID {
                    continue
                }
                // Skip past events that already ended
                if show.end < .now {
                    continue
                }

                let showNorm = normalizeShowTitle(show.title)
                if showNorm == targetNorm ||
                   (targetNorm.count > 4 && (showNorm.contains(targetNorm) || targetNorm.contains(showNorm))) {
                    results.append(RerunItem(channel: channel, entry: show))
                }
            }
        }

        // Deduplicate and sort chronologically
        var seen = Set<String>()
        var unique: [RerunItem] = []
        for item in results.sorted(by: { $0.entry.start < $1.entry.start }) {
            let key = "\(item.channel.id)_\(Int(item.entry.start.timeIntervalSince1970))"
            if !seen.contains(key) {
                seen.insert(key)
                unique.append(item)
            }
        }
        return unique
    }

    private static let parenRegex = try? NSRegularExpression(pattern: #"\(.*?\)|\[.*?\]"#, options: [])
    private static let seasonEpisodeRegex = try? NSRegularExpression(pattern: #"\b(staffel|folge|episode|s\d+|e\d+|hd|live|wh\.|wiederholung)\b"#, options: [.caseInsensitive])

    private func normalizeShowTitle(_ title: String) -> String {
        var s = title.lowercased().trimmingCharacters(in: .whitespacesAndNewlines)
        if let r = Self.parenRegex {
            s = r.stringByReplacingMatches(in: s, range: NSRange(s.startIndex..., in: s), withTemplate: "")
        }
        if let r = Self.seasonEpisodeRegex {
            s = r.stringByReplacingMatches(in: s, range: NSRange(s.startIndex..., in: s), withTemplate: "")
        }
        return s.trimmingCharacters(in: .whitespacesAndNewlines)
    }
}
