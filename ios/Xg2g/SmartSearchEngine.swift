// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// A matching programme item for the smart search results.
struct SmartSearchShowItem: Identifiable, Sendable {
    var id: String { "\(channel.id)_\(entry.id)" }
    let channel: Channel
    let entry: NowNext.Entry
    let isLive: Bool
    let progress: Double?
    let remainingMinutes: Int?
    let formattedBadge: String
}

/// The structured result of a search query, categorizing results into:
/// 1. Channels that matched the name or station number.
/// 2. Shows that are on air right now (LIVE).
/// 3. Shows that air in the future (UPCOMING), chronologically sorted.
struct SmartSearchResult: Sendable {
    let query: String
    let channels: [Channel]
    let liveShows: [SmartSearchShowItem]
    let upcomingShows: [SmartSearchShowItem]

    var isEmpty: Bool {
        channels.isEmpty && liveShows.isEmpty && upcomingShows.isEmpty
    }

    var totalCount: Int {
        channels.count + liveShows.count + upcomingShows.count
    }

    static let empty = SmartSearchResult(query: "", channels: [], liveShows: [], upcomingShows: [])
}

/// Pure, non-isolated engine for contextual searching across channels, now/next, and multi-day EPG.
enum SmartSearchEngine {

    /// Executes search across all available channel data and EPG events.
    static func search(
        query: String,
        channels: [Channel],
        schedule: [String: NowNext],
        fullEpg: [String: [NowNext.Entry]],
        now: Date = .now
    ) -> SmartSearchResult {
        let cleanQuery = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard cleanQuery.count >= 1 else { return .empty }

        // 1. Channel Name / Number Matches
        var matchedChannels: [Channel] = []
        for ch in channels {
            let nameMatch = ch.name.range(of: cleanQuery, options: .caseInsensitive) != nil
            let numberMatch = ch.number?.lowercased() == cleanQuery.lowercased()
            if nameMatch || numberMatch {
                matchedChannels.append(ch)
            }
        }
        matchedChannels.sort { left, right in
            if left.sortKey != right.sortKey { return left.sortKey < right.sortKey }
            return left.name.localizedCaseInsensitiveCompare(right.name) == .orderedAscending
        }

        // 2. Programme Matches (Live vs. Upcoming)
        var liveList: [SmartSearchShowItem] = []
        var upcomingList: [SmartSearchShowItem] = []

        for channel in channels {
            // Collect all unique shows for this channel
            var allShows: [NowNext.Entry] = fullEpg[channel.serviceRef] ?? []

            // Fallback / supplement with live Now/Next if present
            if let nn = schedule[channel.serviceRef] {
                if let nowEntry = nn.now, !allShows.contains(where: { $0.id == nowEntry.id }) {
                    allShows.append(nowEntry)
                }
                if let nextEntry = nn.next, !allShows.contains(where: { $0.id == nextEntry.id }) {
                    allShows.append(nextEntry)
                }
            }

            for show in allShows {
                // Check title & description matches
                let titleMatch = show.title.range(of: cleanQuery, options: .caseInsensitive) != nil
                let descMatch = show.description?.range(of: cleanQuery, options: .caseInsensitive) != nil
                guard titleMatch || descMatch else { continue }

                // Skip shows that ended in the past
                guard show.end > now else { continue }

                if show.start <= now && show.end > now {
                    // JETZT LIVE
                    let prog = show.progress(at: now)
                    let rem = show.remainingMinutes(at: now)
                    liveList.append(
                        SmartSearchShowItem(
                            channel: channel,
                            entry: show,
                            isLive: true,
                            progress: prog,
                            remainingMinutes: rem,
                            formattedBadge: "JETZT LIVE"
                        )
                    )
                } else if show.start > now {
                    // DEMNÄCHST
                    let badge = formatUpcomingTime(show.start)
                    upcomingList.append(
                        SmartSearchShowItem(
                            channel: channel,
                            entry: show,
                            isLive: false,
                            progress: nil,
                            remainingMinutes: nil,
                            formattedBadge: badge
                        )
                    )
                }
            }
        }

        // Sort live shows: Channels first
        liveList.sort { left, right in
            if left.channel.sortKey != right.channel.sortKey { return left.channel.sortKey < right.channel.sortKey }
            return left.channel.name < right.channel.name
        }

        // Sort upcoming shows strictly chronologically
        upcomingList.sort { left, right in
            if left.entry.start != right.entry.start { return left.entry.start < right.entry.start }
            return left.channel.name < right.channel.name
        }

        // Deduplicate upcoming shows with identical title, channel, and start time
        var uniqueUpcoming: [SmartSearchShowItem] = []
        var seenKeys = Set<String>()
        for item in upcomingList {
            let key = "\(item.channel.id)_\(Int(item.entry.start.timeIntervalSince1970))_\(item.entry.title)"
            if seenKeys.insert(key).inserted {
                uniqueUpcoming.append(item)
            }
        }

        return SmartSearchResult(
            query: cleanQuery,
            channels: matchedChannels,
            liveShows: liveList,
            upcomingShows: uniqueUpcoming
        )
    }

    private static func formatUpcomingTime(_ date: Date) -> String {
        let calendar = Calendar.current
        let timeFormatter = DateFormatter()
        timeFormatter.dateFormat = "HH:mm"
        let timeStr = timeFormatter.string(from: date)

        if calendar.isDateInToday(date) {
            return "Heute, \(timeStr) Uhr"
        } else if calendar.isDateInTomorrow(date) {
            return "Morgen, \(timeStr) Uhr"
        } else {
            let df = DateFormatter()
            df.locale = Locale(identifier: "de_DE")
            df.dateFormat = "E, d. MMM • HH:mm"
            return "\(df.string(from: date)) Uhr"
        }
    }
}
