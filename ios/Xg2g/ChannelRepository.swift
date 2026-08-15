// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Reads the channel catalogue and what is on it.
///
/// Now/next is deliberately a *separate* call rather than folded into the
/// channel list: the catalogue changes rarely and the schedule changes
/// constantly, so tying them together would mean refetching every channel to
/// learn that a programme ended.
actor ChannelRepository {

    private let api: APIClient
    private let baseURL: URL?

    init(api: APIClient, baseURL: URL? = nil) {
        self.api = api
        self.baseURL = baseURL
    }

    /// Fetches all available channel bouquets / groups.
    func bouquets() async throws -> [Bouquet] {
        let items: [ChannelWire.BouquetItem] = try await api.send(
            APIRequest(method: .get, path: "services/bouquets")
        )
        return items.compactMap { $0.toDomain() }
    }

    /// The channel list, ordered the way a viewer expects.
    ///
    /// Entries the app cannot use — no name, no service reference — are
    /// dropped here rather than rendered as blank rows.
    func channels(bouquet: String? = nil) async throws -> [Channel] {
        let query = bouquet.map { [URLQueryItem(name: "bouquet", value: $0)] } ?? []
        let services: [ChannelWire.Service] = try await api.send(
            APIRequest(method: .get, path: "services", query: query)
        )

        return services
            .compactMap { $0.toDomain(baseURL: baseURL) }
            .sorted { left, right in
                left.sortKey == right.sortKey
                    ? left.name.localizedCaseInsensitiveCompare(right.name) == .orderedAscending
                    : left.sortKey < right.sortKey
            }
    }

    /// Now and next for the given channels, keyed by service reference.
    ///
    /// The server takes a batch, so the whole visible list costs one request.
    /// An empty input is answered without a call: the endpoint requires at
    /// least one service and would return a 400 that means nothing to a caller
    /// who simply had nothing to ask about.
    func nowNext(for serviceRefs: [String]) async throws -> [String: NowNext] {
        guard !serviceRefs.isEmpty else { return [:] }

        let response: ChannelWire.NowNextResponse = try await api.send(
            APIRequest(
                method: .post,
                path: "services/now-next",
                body: try JSONEncoder().encode(ChannelWire.NowNextRequest(services: serviceRefs)),
                contentType: "application/json"
            )
        )

        return Dictionary(
            response.items.map { ($0.serviceRef, NowNext(item: $0)) },
            // A server that repeated a service reference would otherwise crash
            // the app on a duplicate key. Last one wins; neither is more
            // correct, and neither is worth a trap.
            uniquingKeysWith: { _, last in last }
        )
    }

    /// Fetches the full EPG schedule for all channels or a specific bouquet.
    func epgSchedule(bouquet: String? = nil) async throws -> [String: [NowNext.Entry]] {
        let query = bouquet.map { [URLQueryItem(name: "bouquet", value: $0)] } ?? []
        let items: [ChannelWire.EpgItem] = try await api.send(
            APIRequest(method: .get, path: "epg", query: query)
        )

        var map: [String: [NowNext.Entry]] = [:]
        for item in items {
            if let (serviceRef, entry) = item.toDomain() {
                map[serviceRef, default: []].append(entry)
            }
        }
        for (key, list) in map {
            map[key] = list.sorted { $0.start < $1.start }
        }
        return map
    }
}
