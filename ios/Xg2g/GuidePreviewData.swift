// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

#if DEBUG
import Foundation

/// Sample schedule for the guide previews.
///
/// Times are built relative to "now" so the live states — progress bars, the
/// now-line, the highlighted slot — are actually exercised in the canvas
/// instead of only showing up against a real receiver.
enum GuidePreviewData {

    private static func halfHour(_ date: Date, calendar: Calendar) -> Date {
        var components = calendar.dateComponents([.year, .month, .day, .hour, .minute], from: date)
        components.minute = (components.minute ?? 0) < 30 ? 0 : 30
        return calendar.date(from: components) ?? date
    }

    static func channel(_ number: String, _ name: String) -> Channel {
        Channel(
            id: "preview_\(number)",
            name: name,
            number: number,
            serviceRef: "1:0:19:\(number):0:0:0:0:0:0:",
            logoURL: nil
        )
    }

    static func entry(
        _ title: String,
        startingMinutesFromNow offset: Int,
        lasting duration: Int,
        description: String? = nil
    ) -> NowNext.Entry {
        let start = Date.now.addingTimeInterval(Double(offset) * 60)
        return NowNext.Entry(
            title: title,
            description: description,
            start: start,
            end: start.addingTimeInterval(Double(duration) * 60)
        )
    }

    /// A believable evening: something running, something starting soon.
    static var projection: GuideProjection {
        let ard = channel("1", "Das Erste HD")
        let zdf = channel("2", "ZDF HD")
        let sky = channel("201", "Sky Sport Top Event")
        let puls = channel("14", "PULS 24 HD")

        let schedules = [
            GuideChannelSchedule(channel: ard, shows: [
                entry("Tagesschau", startingMinutesFromNow: -25, lasting: 15),
                entry("Tatort: Der schwarze Troll", startingMinutesFromNow: -10, lasting: 90),
                entry("Tagesthemen", startingMinutesFromNow: 80, lasting: 30)
            ]),
            GuideChannelSchedule(channel: zdf, shows: [
                entry("heute journal", startingMinutesFromNow: -20, lasting: 30),
                entry("Der Bergdoktor", startingMinutesFromNow: 10, lasting: 60),
                entry("Markus Lanz", startingMinutesFromNow: 70, lasting: 75)
            ]),
            GuideChannelSchedule(channel: sky, shows: [
                entry("Bundesliga Konferenz", startingMinutesFromNow: -40, lasting: 120),
                entry("Sky Sport News", startingMinutesFromNow: 80, lasting: 25)
            ]),
            GuideChannelSchedule(channel: puls, shows: [
                entry("ZIB 20", startingMinutesFromNow: -5, lasting: 20),
                entry("Talk im Hangar-7", startingMinutesFromNow: 15, lasting: 90)
            ])
        ]

        let now = Date.now
        var onAir: [GuideEntry] = []
        var buckets: [Date: [GuideEntry]] = [:]
        let calendar = Calendar.current

        for schedule in schedules {
            for show in schedule.shows {
                let guideEntry = GuideEntry(channel: schedule.channel, show: show)
                if show.start <= now, show.end > now { onAir.append(guideEntry) }

                var components = calendar.dateComponents(
                    [.year, .month, .day, .hour, .minute], from: show.start
                )
                components.minute = (components.minute ?? 0) < 30 ? 0 : 30
                let bucket = calendar.date(from: components) ?? show.start
                buckets[bucket, default: []].append(guideEntry)
            }
        }

        // Sorted exactly as `GuideProjectionBuilder` does, so a preview cannot
        // show an ordering the real screen never produces.
        return GuideProjection(
            channels: schedules,
            onAir: onAir.sorted { $0.channel.sortKey < $1.channel.sortKey },
            slots: buckets
                .map { bucket in
                    GuideSlot(
                        start: bucket.key,
                        entries: bucket.value.sorted {
                            $0.show.start != $1.show.start
                                ? $0.show.start < $1.show.start
                                : $0.channel.sortKey < $1.channel.sortKey
                        }
                    )
                }
                .sorted { $0.start < $1.start },
            // Bucketed to a clean half hour, as `GuideProjectionBuilder` always
            // does — otherwise the preview's ruler shows times like 11:03 that
            // the real screen never produces.
            windowStart: halfHour(now.addingTimeInterval(-60 * 60), calendar: calendar),
            windowEnd: halfHour(now.addingTimeInterval(-60 * 60), calendar: calendar)
                .addingTimeInterval(6 * 60 * 60)
        )
    }

}
#endif
