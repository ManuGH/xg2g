// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Where the guide's visible window begins.
///
/// These are anchors, not filters. "20:15" does not hide everything else — it
/// moves the window to where the evening starts, which is how people actually
/// talk about German broadcast schedules.
enum GuideAnchor: String, CaseIterable, Identifiable, Sendable {
    case now = "Jetzt"
    case primeTime = "20:15"
    case lateNight = "22:00"
    case allDay = "Ganztägig"

    var id: String { rawValue }

    /// Hours covered from the anchor. `allDay` spans whatever is left of the day.
    var span: TimeInterval? {
        switch self {
        case .now, .primeTime, .lateNight: return 6 * 3600
        case .allDay: return nil
        }
    }
}

/// How the guide is presented. Each mode answers a different question, which is
/// why all three exist rather than one being a variant of another.
enum GuideMode: String, CaseIterable, Identifiable, Sendable {
    /// "What is on right now?" — one row per channel, live progress.
    case onAir = "Jetzt"
    /// "What is coming up?" — programmes grouped into half-hour slots.
    case timeline = "Zeitschiene"
    /// "What does the evening look like?" — channels × time, blocks to scale.
    case grid = "Raster"

    var id: String { rawValue }

    var symbol: String {
        switch self {
        case .onAir: return "dot.radiowaves.left.and.right"
        case .timeline: return "list.bullet.indent"
        case .grid: return "rectangle.split.3x1"
        }
    }
}

/// One programme on one channel — the atom every mode is built from.
struct GuideEntry: Identifiable, Sendable {
    let channel: Channel
    let show: NowNext.Entry

    var id: String { "\(channel.id)_\(show.id)" }
}

/// A channel and everything it airs inside the window.
struct GuideChannelSchedule: Identifiable, Sendable {
    let channel: Channel
    let shows: [NowNext.Entry]

    var id: String { channel.id }
}

/// Programmes that start inside the same half hour.
struct GuideSlot: Identifiable, Sendable {
    let start: Date
    let entries: [GuideEntry]

    var id: TimeInterval { start.timeIntervalSince1970 }
}

/// Everything the guide screen renders, computed once per input change.
///
/// All three presentations are derived here rather than in `body`: the genre
/// classification and text search below are the expensive part, and doing them
/// per view evaluation is what made this screen stutter.
struct GuideProjection: Sendable {
    let channels: [GuideChannelSchedule]
    let onAir: [GuideEntry]
    let slots: [GuideSlot]
    let windowStart: Date
    let windowEnd: Date

    static let empty = GuideProjection(
        channels: [],
        onAir: [],
        slots: [],
        windowStart: .distantPast,
        windowEnd: .distantPast
    )

    var isEmpty: Bool { channels.isEmpty }

    var totalShowCount: Int {
        channels.reduce(0) { $0 + $1.shows.count }
    }
}

enum GuideProjectionBuilder {

    /// Builds the projection. Pure and `nonisolated` so it can run off the main actor.
    static func build(
        channels: [Channel],
        epg: [String: [NowNext.Entry]],
        dayOffset: Int,
        anchor: GuideAnchor,
        genre: EpgGenre,
        searchText: String,
        now: Date
    ) -> GuideProjection {
        let calendar = Calendar.current
        let today = calendar.startOfDay(for: now)
        guard let dayStart = calendar.date(byAdding: .day, value: dayOffset, to: today),
              let dayEnd = calendar.date(byAdding: .day, value: 1, to: dayStart) else {
            return .empty
        }

        let windowStart = anchorStart(anchor, dayStart: dayStart, dayEnd: dayEnd, now: now, calendar: calendar)
        let windowEnd = anchor.span.map { min(windowStart.addingTimeInterval($0), dayEnd) } ?? dayEnd

        let query = searchText.trimmingCharacters(in: .whitespaces)
        let hasQuery = !query.isEmpty
        let hasGenreFilter = genre != .all

        var schedules: [GuideChannelSchedule] = []
        schedules.reserveCapacity(channels.count)

        var onAir: [GuideEntry] = []
        var slotBuckets: [Date: [GuideEntry]] = [:]

        for channel in channels {
            let allShows = epg[channel.serviceRef] ?? []

            // Hoisted: the channel name is identical for every show below.
            let channelMatchesQuery = hasQuery
                && channel.name.range(of: query, options: .caseInsensitive) != nil

            var shows: [NowNext.Entry] = []

            for show in allShows {
                // 1. Inside the visible window
                guard show.start < windowEnd, show.end > windowStart else { continue }

                // 2. Genre
                if hasGenreFilter, !show.matches(genre: genre, channelName: channel.name) {
                    continue
                }

                // 3. Search. `range(of:options:)` rather than lowercasing — the
                //    latter allocated a fresh String per title and description.
                if hasQuery, !channelMatchesQuery {
                    let titleMatch = show.title.range(of: query, options: .caseInsensitive) != nil
                    let descMatch = show.description?.range(of: query, options: .caseInsensitive) != nil
                    guard titleMatch || descMatch else { continue }
                }

                shows.append(show)

                let entry = GuideEntry(channel: channel, show: show)

                if show.start <= now, show.end > now {
                    onAir.append(entry)
                }

                let bucket = halfHourBucket(for: max(show.start, windowStart), calendar: calendar)
                slotBuckets[bucket, default: []].append(entry)
            }

            if !shows.isEmpty {
                shows.sort { $0.start < $1.start }
                schedules.append(GuideChannelSchedule(channel: channel, shows: shows))
            }
        }

        let slots = slotBuckets
            .map { GuideSlot(start: $0.key, entries: $0.value.sorted(by: startThenChannel)) }
            .sorted { $0.start < $1.start }

        return GuideProjection(
            channels: schedules,
            onAir: onAir.sorted(by: channelOrder),
            slots: slots,
            windowStart: windowStart,
            windowEnd: windowEnd
        )
    }

    /// Channel number first, name as the tiebreaker — the order people expect
    /// from a receiver, not the order the EPG happened to arrive in.
    private static func channelOrder(_ lhs: GuideEntry, _ rhs: GuideEntry) -> Bool {
        if lhs.channel.sortKey != rhs.channel.sortKey {
            return lhs.channel.sortKey < rhs.channel.sortKey
        }
        return lhs.channel.name < rhs.channel.name
    }

    /// Start time first inside a slot.
    ///
    /// The row's clock reading is the column the eye follows, so it has to run
    /// downwards. Sorting a slot by channel instead produced 11:35, 11:50,
    /// 11:40, 11:55 under one header — two orderings fighting each other.
    private static func startThenChannel(_ lhs: GuideEntry, _ rhs: GuideEntry) -> Bool {
        if lhs.show.start != rhs.show.start {
            return lhs.show.start < rhs.show.start
        }
        return channelOrder(lhs, rhs)
    }

    private static func halfHourBucket(for date: Date, calendar: Calendar) -> Date {
        var components = calendar.dateComponents([.year, .month, .day, .hour, .minute], from: date)
        components.minute = (components.minute ?? 0) < 30 ? 0 : 30
        return calendar.date(from: components) ?? date
    }

    private static func anchorStart(
        _ anchor: GuideAnchor,
        dayStart: Date,
        dayEnd: Date,
        now: Date,
        calendar: Calendar
    ) -> Date {
        switch anchor {
        case .allDay:
            return dayStart

        case .now:
            // Only meaningful for today; on another day the evening is the
            // sensible place to open.
            guard now >= dayStart, now < dayEnd else {
                return time(hour: 20, on: dayStart, calendar: calendar) ?? dayStart
            }
            return halfHourBucket(for: now, calendar: calendar)

        case .primeTime:
            return time(hour: 20, on: dayStart, calendar: calendar) ?? dayStart

        case .lateNight:
            return time(hour: 22, on: dayStart, calendar: calendar) ?? dayStart
        }
    }

    private static func time(hour: Int, on day: Date, calendar: Calendar) -> Date? {
        var components = calendar.dateComponents([.year, .month, .day], from: day)
        components.hour = hour
        components.minute = 0
        return calendar.date(from: components)
    }
}
