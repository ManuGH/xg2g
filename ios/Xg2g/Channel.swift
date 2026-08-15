// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// One channel, as the app understands it.
///
/// The wire type has more fields than this — `enabled`, `resolution`, `codec`,
/// `group`. They are not carried here because nothing in the app decides
/// anything with them yet, and a model that mirrors a schema rather than a use
/// keeps every future change honest only by accident.
///
/// `serviceRef` is the identifier that matters: it is what a stream intent and
/// a now/next lookup are keyed on. `id` is the catalogue key and is not
/// interchangeable with it.
struct Channel: Identifiable, Hashable, Equatable, Sendable {
    let id: String
    let name: String
    let number: String?
    let serviceRef: String
    let logoURL: URL?

    /// Channels arrive with a `number` like "101" that is really an ordering
    /// key. Sorting on the string would put 100 before 2.
    var sortKey: Int { number.flatMap(Int.init) ?? Int.max }
}

/// What is on now, and what follows.
struct NowNext: Equatable, Sendable {
    struct Entry: Identifiable, Equatable, Sendable {
        var id: String { "\(start.timeIntervalSince1970)_\(title)" }
        let title: String
        let description: String?
        let start: Date
        let end: Date

        private static let timeFormatter: DateFormatter = {
            let f = DateFormatter()
            f.dateFormat = "HH:mm"
            f.timeZone = .current
            return f
        }()

        /// How far through the programme we are, 0…1. `nil` before it starts or
        /// after it ends, so a caller cannot mistake "not on" for "just began".
        func progress(at now: Date) -> Double? {
            let total = end.timeIntervalSince(start)
            guard total > 0, now >= start, now <= end else { return nil }
            return now.timeIntervalSince(start) / total
        }

        /// Minutes left in the currently running programme.
        func remainingMinutes(at now: Date) -> Int? {
            guard now >= start, now <= end else { return nil }
            let secondsLeft = end.timeIntervalSince(now)
            return max(1, Int(secondsLeft / 60))
        }

        var formattedStartTime: String {
            Self.timeFormatter.string(from: start)
        }

        var formattedEndTime: String {
            Self.timeFormatter.string(from: end)
        }

        var formattedTimeRange: String {
            "\(Self.timeFormatter.string(from: start)) – \(Self.timeFormatter.string(from: end))"
        }

        var formattedDayHeader: String {
            let calendar = Calendar.current
            if calendar.isDateInToday(start) {
                return "HEUTE"
            } else if calendar.isDateInTomorrow(start) {
                return "MORGEN"
            } else {
                let f = DateFormatter()
                f.locale = Locale(identifier: "de_DE")
                f.dateFormat = "EEEE, d. MMMM"
                return f.string(from: start).uppercased()
            }
        }

        var dayIdentifier: String {
            let f = DateFormatter()
            f.dateFormat = "yyyy-MM-dd"
            return f.string(from: start)
        }
    }

    let serviceRef: String
    let now: Entry?
    let next: Entry?
}

/// A bouquet / channel group (e.g. "Favorites", "HD", "Sports").
struct Bouquet: Identifiable, Hashable, Equatable, Sendable {
    let id: String
    let name: String
    let servicesCount: Int

    init(id: String? = nil, name: String, servicesCount: Int = 0) {
        self.id = id ?? name
        self.name = name
        self.servicesCount = servicesCount
    }
}

// MARK: - Wire

enum ChannelWire {

    struct BouquetItem: Decodable, Sendable {
        let name: String?
        let services: Int?

        func toDomain() -> Bouquet? {
            guard let name = name?.trimmingCharacters(in: .whitespaces), !name.isEmpty else { return nil }
            return Bouquet(name: name, servicesCount: services ?? 0)
        }
    }

    struct EpgItem: Decodable, Sendable {
        let serviceRef: String?
        let title: String?
        let desc: String?
        let start: Int?
        let end: Int?

        func toDomain() -> (String, NowNext.Entry)? {
            guard let serviceRef, let title, let start, let end else { return nil }
            let sanitizedDesc: String? = {
                guard let raw = desc?.trimmingCharacters(in: .whitespacesAndNewlines), !raw.isEmpty else {
                    return nil
                }
                var text = raw
                    .replacingOccurrences(of: "\\n", with: "\n")
                    .replacingOccurrences(of: "\\r", with: "")
                    .replacingOccurrences(of: "\\t", with: "\t")
                while text.contains("\n\n\n") {
                    text = text.replacingOccurrences(of: "\n\n\n", with: "\n\n")
                }
                return text.trimmingCharacters(in: .whitespacesAndNewlines)
            }()

            let entry = NowNext.Entry(
                title: title.replacingOccurrences(of: "\\n", with: " ").trimmingCharacters(in: .whitespacesAndNewlines),
                description: sanitizedDesc,
                start: Date(timeIntervalSince1970: TimeInterval(start)),
                end: Date(timeIntervalSince1970: TimeInterval(end))
            )
            return (serviceRef, entry)
        }
    }

    /// The server sends every field as optional. A channel without a name or a
    /// service reference cannot be displayed or played, so it is dropped at the
    /// boundary rather than carried inward as a half-value.
    struct Service: Decodable, Sendable {
        let id: String?
        let name: String?
        let number: String?
        let serviceRef: String?
        let logoUrl: String?

        func toDomain(baseURL: URL? = nil) -> Channel? {
            guard let name = name?.trimmingCharacters(in: .whitespaces), !name.isEmpty,
                  let serviceRef = serviceRef?.trimmingCharacters(in: .whitespaces), !serviceRef.isEmpty
            else { return nil }

            let resolvedLogo: URL?
            if let rawLogo = logoUrl?.trimmingCharacters(in: .whitespacesAndNewlines), !rawLogo.isEmpty {
                if rawLogo.hasPrefix("http://") || rawLogo.hasPrefix("https://") {
                    resolvedLogo = URL(string: rawLogo)
                } else if let baseURL {
                    let path = rawLogo.hasPrefix("/") ? String(rawLogo.dropFirst()) : rawLogo
                    resolvedLogo = baseURL.appendingPathComponent(path)
                } else {
                    resolvedLogo = URL(string: rawLogo)
                }
            } else if let baseURL {
                // Fallback: Default OpenWebif/xg2g logo path by normalized service reference
                let sanitizedRef = serviceRef.replacingOccurrences(of: ":", with: "_").trimmingCharacters(in: CharacterSet(charactersIn: "_"))
                resolvedLogo = baseURL.appendingPathComponent("logos/\(sanitizedRef).png")
            } else {
                resolvedLogo = nil
            }

            return Channel(
                id: id?.isEmpty == false ? id! : serviceRef,
                name: name,
                number: number?.isEmpty == false ? number : nil,
                serviceRef: serviceRef,
                logoURL: resolvedLogo
            )
        }
    }

    struct NowNextRequest: Encodable, Sendable {
        let services: [String]
    }

    struct NowNextResponse: Decodable, Sendable {
        let items: [Item]

        struct Item: Decodable, Sendable {
            let serviceRef: String
            let now: Entry?
            let next: Entry?
        }

        struct Entry: Decodable, Sendable {
            let title: String
            let desc: String?
            let start: Int
            let end: Int

            func toDomain() -> NowNext.Entry {
                let sanitizedDesc: String? = {
                    guard let raw = desc?.trimmingCharacters(in: .whitespacesAndNewlines), !raw.isEmpty else {
                        return nil
                    }
                    var text = raw
                        .replacingOccurrences(of: "\\n", with: "\n")
                        .replacingOccurrences(of: "\\r", with: "")
                        .replacingOccurrences(of: "\\t", with: "\t")
                    while text.contains("\n\n\n") {
                        text = text.replacingOccurrences(of: "\n\n\n", with: "\n\n")
                    }
                    return text.trimmingCharacters(in: .whitespacesAndNewlines)
                }()

                let sanitizedTitle = title
                    .replacingOccurrences(of: "\\n", with: " ")
                    .replacingOccurrences(of: "\\r", with: "")
                    .trimmingCharacters(in: .whitespacesAndNewlines)

                return NowNext.Entry(
                    title: sanitizedTitle,
                    description: sanitizedDesc,
                    start: Date(timeIntervalSince1970: TimeInterval(start)),
                    end: Date(timeIntervalSince1970: TimeInterval(end))
                )
            }
        }
    }
}

extension NowNext {
    init(item: ChannelWire.NowNextResponse.Item) {
        self.init(
            serviceRef: item.serviceRef,
            now: item.now?.toDomain(),
            next: item.next?.toDomain()
        )
    }
}

extension Sequence where Element: Hashable {
    func uniqued() -> [Element] {
        var seen = Set<Element>()
        return filter { seen.insert($0).inserted }
    }
}
