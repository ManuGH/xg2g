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
struct Channel: Identifiable, Equatable, Sendable {
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
    struct Entry: Equatable, Sendable {
        let title: String
        let description: String?
        let start: Date
        let end: Date

        /// How far through the programme we are, 0…1. `nil` before it starts or
        /// after it ends, so a caller cannot mistake "not on" for "just began".
        func progress(at now: Date) -> Double? {
            let total = end.timeIntervalSince(start)
            guard total > 0, now >= start, now <= end else { return nil }
            return now.timeIntervalSince(start) / total
        }
    }

    let serviceRef: String
    let now: Entry?
    let next: Entry?
}

// MARK: - Wire

enum ChannelWire {

    /// The server sends every field as optional. A channel without a name or a
    /// service reference cannot be displayed or played, so it is dropped at the
    /// boundary rather than carried inward as a half-value.
    struct Service: Decodable, Sendable {
        let id: String?
        let name: String?
        let number: String?
        let serviceRef: String?
        let logoUrl: String?

        func toDomain() -> Channel? {
            guard let name = name?.trimmingCharacters(in: .whitespaces), !name.isEmpty,
                  let serviceRef = serviceRef?.trimmingCharacters(in: .whitespaces), !serviceRef.isEmpty
            else { return nil }

            return Channel(
                // The catalogue id is the stable identity; a channel without one
                // falls back to its service reference, which is unique too.
                id: id?.isEmpty == false ? id! : serviceRef,
                name: name,
                number: number?.isEmpty == false ? number : nil,
                serviceRef: serviceRef,
                logoURL: logoUrl.flatMap(URL.init(string:))
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

        /// `start` and `end` are Unix seconds here, not RFC 3339 — this
        /// endpoint speaks epoch integers while the pairing endpoints speak
        /// timestamps. Decoded as integers on purpose rather than routed
        /// through the date strategy, which would silently fail on a number.
        struct Entry: Decodable, Sendable {
            let title: String
            let desc: String?
            let start: Int
            let end: Int

            func toDomain() -> NowNext.Entry {
                NowNext.Entry(
                    title: title,
                    description: desc?.isEmpty == false ? desc : nil,
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
